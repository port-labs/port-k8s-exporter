package handlers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/go-containerregistry/pkg/authn"
	guuid "github.com/google/uuid"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/crd"
	"github.com/port-labs/port-k8s-exporter/pkg/image"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/metrics"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"github.com/port-labs/port-k8s-exporter/pkg/signal"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
)

type ControllersHandler struct {
	controllers      []*k8s.Controller
	informersFactory dynamicinformer.DynamicSharedInformerFactory
	stateKey         string
	portClient       *cli.PortClient
	stopCh           chan struct{}
	isStopped        bool
	portConfig       *port.IntegrationAppConfig
}

type ResyncType string

const (
	INITIAL_RESYNC   ResyncType = "initial resync"
	SCHEDULED_RESYNC ResyncType = "scheduled resync"
	MAPPING_CHANGED  ResyncType = "mapping changed"
)

var controllerHandler *ControllersHandler

func NewControllersHandler(exporterConfig *port.Config, portConfig *port.IntegrationAppConfig, k8sClient *k8s.Client, portClient *cli.PortClient) *ControllersHandler {
	informersFactory := dynamicinformer.NewDynamicSharedInformerFactory(k8sClient.DynamicClient, 0)

	crd.AutodiscoverCRDsToActions(portConfig, k8sClient.ApiExtensionClient, portClient)

	// Set up image OS enricher if enabled
	var imageEnricher *image.Enricher
	if config.ApplicationConfig.ImageOsDetectionEnabled {
		detector := image.NewDetector(authn.DefaultKeychain, config.ApplicationConfig.ImageOsDetectionConcurrency)
		imageEnricher = image.NewEnricher(detector, true)
		logger.Info("Image OS detection enabled")
	}

	aggResources := make(map[string][]port.KindConfig)
	for _, resource := range portConfig.Resources {
		kindConfig := port.KindConfig{Selector: resource.Selector, Port: resource.Port}
		if _, ok := aggResources[resource.Kind]; ok {
			aggResources[resource.Kind] = append(aggResources[resource.Kind], kindConfig)
		} else {
			aggResources[resource.Kind] = []port.KindConfig{kindConfig}
		}
	}

	controllers := make([]*k8s.Controller, 0, len(portConfig.Resources))

	for kind, kindConfigs := range aggResources {
		var gvr schema.GroupVersionResource
		gvr, err := k8s.GetGVRFromResource(k8sClient.DiscoveryMapper, kind)
		if err != nil {
			logger.Errorf("Error getting GVR, skip handling for resource '%s': %s.", kind, err.Error())
			continue
		}

		informer := informersFactory.ForResource(gvr)
		controller := k8s.NewController(port.AggregatedResource{Kind: kind, KindConfigs: kindConfigs}, informer, portConfig, config.ApplicationConfig, imageEnricher)
		controllers = append(controllers, controller)
	}

	controllersHandler := &ControllersHandler{
		controllers:      controllers,
		informersFactory: informersFactory,
		stateKey:         exporterConfig.StateKey,
		portClient:       portClient,
		stopCh:           signal.SetupSignalHandler(),
		portConfig:       portConfig,
	}

	return controllersHandler
}

func (c *ControllersHandler) Handle(resyncType ResyncType) {
	eventLogger := logger.GetEventLogger(guuid.NewString())
	eventLogger.Infow(fmt.Sprintf("Starting resync due to %s", resyncType), "stateKey", c.stateKey)
	eventLogger.Infow("Starting informers")
	c.informersFactory.Start(c.stopCh)

	resyncResults, err := syncAllControllers(c, eventLogger)
	if err != nil {
		eventLogger.Errorw("Error syncing controllers", "resyncType", resyncType, "error", err.Error())
		return
	}

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	go func() {
		<-c.stopCh
		cancelCtx()
	}()

	blueprintToKinds := buildBlueprintToKinds(c.portConfig.Resources)
	mergedSet, eligibleBlueprints, skippedKinds := computeDeletionPlan(resyncResults.ControllerResults, blueprintToKinds)

	if len(eligibleBlueprints) == 0 {
		eventLogger.Warnw("Skipping delete of stale entities; no blueprints eligible", "skippedKinds", skippedKinds)
		return
	}

	eligibleBlueprintKeys := make([]string, 0, len(eligibleBlueprints))
	for blueprint := range eligibleBlueprints {
		eligibleBlueprintKeys = append(eligibleBlueprintKeys, blueprint)
	}

	if len(skippedKinds) > 0 {
		eventLogger.Warnw("Partial stale entity cleanup; some kinds failed sync", "skippedKinds", skippedKinds, "eligibleBlueprints", eligibleBlueprintKeys)
	}

	eventLogger.Infow("Deleting stale entities", "eligibleBlueprints", eligibleBlueprintKeys, "skippedKinds", skippedKinds)
	err = c.runDeleteStaleEntities(ctx, mergedSet, eligibleBlueprints, eventLogger)
	if err != nil {
		eventLogger.Errorw("Error deleting stale entities", "error", err.Error())
	}
	eventLogger.Infow("Done deleting stale entities")
	metrics.SetSuccessStatusConditionally(metrics.MetricKindReconciliation, metrics.MetricPhaseDelete, err == nil)
}

func RunResync(exporterConfig *port.Config, k8sClient *k8s.Client, portClient *cli.PortClient, resyncType ResyncType) error {
	if controllerHandler != (*ControllersHandler)(nil) {
		controllerHandler.Stop()
	}

	newControllersHandler, resyncErr := metrics.MeasureResync(func() (*ControllersHandler, error) {
		i, err := integration.GetIntegration(portClient, exporterConfig.StateKey)
		if err != nil {
			metrics.SetSuccessStatus(metrics.MetricKindResync, metrics.MetricPhaseResync, metrics.PhaseFailed)
			return nil, fmt.Errorf("error getting Port integration: %v", err)
		}
		if i.Config == nil {
			metrics.SetSuccessStatus(metrics.MetricKindResync, metrics.MetricPhaseResync, metrics.PhaseFailed)
			return nil, errors.New("integration config is nil")
		}
		if !i.Config.AllowAllEnvironmentVariablesInJQ {
			config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = i.Config.AllowAllEnvironmentVariablesInJQ
		}
		if i.Config.AllowedEnvironmentVariablesInJQ != nil {
			config.ApplicationConfig.AllowedEnvironmentVariablesInJQ = i.Config.AllowedEnvironmentVariablesInJQ
		}
		newHandler := NewControllersHandler(exporterConfig, i.Config, k8sClient, portClient)
		newHandler.Handle(resyncType)
		return newHandler, nil
	})
	controllerHandler = newControllersHandler

	return resyncErr
}

func syncAllControllers(c *ControllersHandler, eventLogger *zap.SugaredLogger) (*FullResyncResults, error) {
	return metrics.MeasureDuration(metrics.MetricKindResync, metrics.MetricPhaseResync, func(phase string) (*FullResyncResults, error) {
		controllerResults := make([]ControllerSyncResult, 0, len(c.controllers))
		var resultsMu sync.Mutex
		allKindsSucceeded := true
		var syncWg sync.WaitGroup

		for _, controller := range c.controllers {
			controller := controller
			go func() {
				<-c.stopCh
				logger.Info("Shutting down controllers")
				controller.Shutdown()
			}()

			metrics.InitializeMetricsForController(&controller.Resource)
			metrics.MeasureDuration(metrics.GetKindLabel(controller.Resource.Kind, nil), metrics.MetricPhaseExtract, func(phase string) (struct{}, error) {
				eventLogger.Infow(fmt.Sprintf("Waiting for informer cache to sync for resource '%s'", controller.Resource.Kind))
				if err := controller.WaitForCacheSync(c.stopCh); err != nil {
					eventLogger.Errorw("Error while waiting for informer cache sync", "error", err.Error())
				}
				// For compatibility to other object kind metrics, we add
				// this metric per kind and not once per resource
				for kindIndex := range controller.Resource.KindConfigs {
					metrics.AddObjectCount(metrics.GetKindLabel(controller.Resource.Kind, &kindIndex), metrics.MetricRawExtractedResult, phase, float64(controller.InitialSyncWorkqueueLen()/len(controller.Resource.KindConfigs)))
				}
				return struct{}{}, nil
			})

			recordResult := func(entitiesSet map[string]interface{}, shouldDeleteStaleEntities bool) {
				resultsMu.Lock()
				defer resultsMu.Unlock()
				controllerResults = append(controllerResults, ControllerSyncResult{
					Kind:                      controller.Resource.Kind,
					EntitiesSet:               entitiesSet,
					ShouldDeleteStaleEntities: shouldDeleteStaleEntities,
				})
				if !shouldDeleteStaleEntities {
					allKindsSucceeded = false
				}
			}

			if c.portConfig.CreateMissingRelatedEntities {
				syncWg.Add(1)
				go func() {
					defer syncWg.Done()
					controllerEntitiesSet, controllerShouldDeleteStaleEntities := syncController(controller, c, eventLogger)
					recordResult(controllerEntitiesSet, controllerShouldDeleteStaleEntities)
				}()
				continue
			}
			controllerEntitiesSet, controllerShouldDeleteStaleEntities := syncController(controller, c, eventLogger)
			recordResult(controllerEntitiesSet, controllerShouldDeleteStaleEntities)
		}
		syncWg.Wait()
		metrics.SetSuccessStatusConditionally(metrics.MetricKindResync, phase, allKindsSucceeded)

		return &FullResyncResults{
			ControllerResults: controllerResults,
		}, nil
	})
}

func syncController(controller *k8s.Controller, c *ControllersHandler, eventLogger *zap.SugaredLogger) (map[string]interface{}, bool) {
	eventLogger.Infow(fmt.Sprintf("Starting full initial resync for resource '%s'", controller.Resource.Kind))
	initialSyncResult := controller.RunInitialSync(eventLogger)
	eventLogger.Infow(fmt.Sprintf("Done full initial resync, starting live events sync for resource '%s'", controller.Resource.Kind))
	controller.RunEventsSync(1, c.stopCh)
	if len(initialSyncResult.RawDataExamples) > 0 {
		err := integration.PostIntegrationKindExample(c.portClient, c.stateKey, controller.Resource.Kind, initialSyncResult.RawDataExamples)
		if err != nil {
			eventLogger.Warnw(fmt.Sprintf("failed to post integration kind example: %s", err.Error()))
		}
	}
	if initialSyncResult.EntitiesSet != nil {
		return initialSyncResult.EntitiesSet, initialSyncResult.ShouldDeleteStaleEntities
	}

	return map[string]interface{}{}, initialSyncResult.ShouldDeleteStaleEntities
}

func (c *ControllersHandler) runDeleteStaleEntities(ctx context.Context, existingEntitiesSet map[string]interface{}, eligibleBlueprints map[string]bool, eventLogger *zap.SugaredLogger) error {
	relationsByBlueprint := make(map[string]map[string]port.Relation, len(eligibleBlueprints))
	for blueprintID := range eligibleBlueprints {
		bp, err := cli.GetBlueprint(c.portClient, blueprintID)
		if err != nil {
			eventLogger.Warnw("Could not fetch blueprint for delete ordering; using alphabetical fallback for this blueprint", "blueprint", blueprintID, "error", err.Error())
			relationsByBlueprint[blueprintID] = nil
			continue
		}
		relationsByBlueprint[blueprintID] = bp.Relations
	}

	blueprintDeleteOrder := sortBlueprintsForDeletion(relationsByBlueprint)
	eventLogger.Infow("Computed blueprint delete order", "blueprintDeleteOrder", blueprintDeleteOrder)

	_, err := metrics.MeasureDuration(metrics.MetricKindReconciliation, metrics.MetricPhaseDelete, func(phase string) (struct{}, error) {
		err := c.portClient.DeleteStaleEntities(ctx, c.stateKey, existingEntitiesSet, blueprintDeleteOrder)
		if err != nil {
			eventLogger.Errorw("error deleting stale entities", "error", err.Error())
			return struct{}{}, err
		}
		return struct{}{}, nil
	})
	return err
}

func (c *ControllersHandler) Stop() {
	if c.isStopped {
		return
	}

	logger.Info("Stopping controllers")
	close(c.stopCh)
	c.isStopped = true
}
