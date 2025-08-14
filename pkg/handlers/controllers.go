package handlers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/crd"
	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/metrics"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"github.com/port-labs/port-k8s-exporter/pkg/signal"
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

type FullResyncResults struct {
	EntitiesSets              []map[string]interface{}
	ShouldDeleteStaleEntities bool
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
		controller := k8s.NewController(port.AggregatedResource{Kind: kind, KindConfigs: kindConfigs}, informer, portConfig, config.ApplicationConfig)
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

func (c *ControllersHandler) Handle(resyncType string) {
	logger.Infof("Starting resync due to %s", resyncType)
	logger.Info("Starting informers")
	c.informersFactory.Start(c.stopCh)

	resyncResults, err := syncAllControllers(c)
	if err != nil {
		logger.Errorf("Error syncing controllers: %s", err.Error())
		return
	}

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	go func() {
		<-c.stopCh
		cancelCtx()
	}()

	if resyncResults.ShouldDeleteStaleEntities {
		logger.Info("Deleting stale entities")
		c.runDeleteStaleEntities(ctx, resyncResults.EntitiesSets)
		logger.Info("Done deleting stale entities")
		metrics.SetSuccess(metrics.MetricKindReconciliation, metrics.MetricPhaseDelete, 1)
	} else {
		logger.Warning("Skipping delete of stale entities due to a failure in getting all current entities from k8s")
		metrics.SetSuccess(metrics.MetricKindReconciliation, metrics.MetricPhaseDelete, 0)
	}
}

func RunResync(exporterConfig *port.Config, k8sClient *k8s.Client, portClient *cli.PortClient, resyncType ResyncType) error {
	if controllerHandler != (*ControllersHandler)(nil) {
		controllerHandler.Stop()
	}

	newController, resyncErr := metrics.MeasureResync(func() (*ControllersHandler, error) {
		i, err := integration.GetIntegration(portClient, exporterConfig.StateKey)
		if err != nil {
			metrics.SetSuccess(metrics.MetricKindResync, metrics.MetricPhaseResync, 0)
			return nil, fmt.Errorf("error getting Port integration: %v", err)
		}
		if i.Config == nil {
			metrics.SetSuccess(metrics.MetricKindResync, metrics.MetricPhaseResync, 0)
			return nil, errors.New("integration config is nil")
		}

		newHandler := NewControllersHandler(exporterConfig, i.Config, k8sClient, portClient)
		newHandler.Handle(string(resyncType))
		return newHandler, nil
	})
	controllerHandler = newController

	return resyncErr
}

func syncAllControllers(c *ControllersHandler) (*FullResyncResults, error) {
	return metrics.MeasureDuration(metrics.MetricKindResync, metrics.MetricPhaseResync, func(phase string) (*FullResyncResults, error) {
		currentEntitiesSets := make([]map[string]interface{}, 0)
		shouldDeleteStaleEntities := true
		var syncWg sync.WaitGroup

		for _, controller := range c.controllers {
			go func() {
				<-c.stopCh
				logger.Info("Shutting down controllers")
				controller.Shutdown()
			}()

			metrics.MeasureDuration(metrics.GetKindLabel(controller.Resource.Kind, nil), metrics.MetricPhaseExtract, func(phase string) (struct{}, error) {
				logger.Infof("Waiting for informer cache to sync for resource '%s'", controller.Resource.Kind)
				if err := controller.WaitForCacheSync(c.stopCh); err != nil {
					logger.Fatalf("Error while waiting for informer cache sync: %s", err.Error())
				}
				// For compatibility to other object kind metrics, we add
				// this metric per kind and not once per resource
				for kindIndex := range controller.Resource.KindConfigs {
					metrics.AddObjectCount(metrics.GetKindLabel(controller.Resource.Kind, &kindIndex), metrics.MetricRawExtractedResult, phase, float64(controller.WorkqueueLen()/len(controller.Resource.KindConfigs)))
				}
				return struct{}{}, nil
			})

			if c.portConfig.CreateMissingRelatedEntities {
				syncWg.Add(1)
				go func() {
					defer syncWg.Done()
					controllerEntitiesSet, controllerShouldDeleteStaleEntities := syncController(controller, c)
					currentEntitiesSets = append(currentEntitiesSets, controllerEntitiesSet)
					shouldDeleteStaleEntities = shouldDeleteStaleEntities && controllerShouldDeleteStaleEntities
				}()
				continue
			}
			controllerEntitiesSet, controllerShouldDeleteStaleEntities := syncController(controller, c)
			currentEntitiesSets = append(currentEntitiesSets, controllerEntitiesSet)
			shouldDeleteStaleEntities = shouldDeleteStaleEntities && controllerShouldDeleteStaleEntities
		}
		syncWg.Wait()
		success := 1
		if !shouldDeleteStaleEntities {
			success = 0
		}
		metrics.SetSuccess(metrics.MetricKindResync, phase, float64(success))

		return &FullResyncResults{
			EntitiesSets:              currentEntitiesSets,
			ShouldDeleteStaleEntities: shouldDeleteStaleEntities,
		}, nil
	})
}

func syncController(controller *k8s.Controller, c *ControllersHandler) (map[string]interface{}, bool) {
	logger.Infof("Starting full initial resync for resource '%s'", controller.Resource.Kind)
	metrics.InitializeMetricsForController(&controller.Resource)
	initialSyncResult := controller.RunInitialSync()
	logger.Infof("Done full initial resync, starting live events sync for resource '%s'", controller.Resource.Kind)
	controller.RunEventsSync(1, c.stopCh)
	if len(initialSyncResult.RawDataExamples) > 0 {
		err := integration.PostIntegrationKindExample(c.portClient, c.stateKey, controller.Resource.Kind, initialSyncResult.RawDataExamples)
		if err != nil {
			logger.Warningf("failed to post integration kind example: %s", err.Error())
		}
	}
	if initialSyncResult.EntitiesSet != nil {
		return initialSyncResult.EntitiesSet, initialSyncResult.ShouldDeleteStaleEntities
	}

	return map[string]interface{}{}, initialSyncResult.ShouldDeleteStaleEntities
}

func (c *ControllersHandler) runDeleteStaleEntities(ctx context.Context, currentEntitiesSet []map[string]interface{}) {
	metrics.MeasureDuration(metrics.MetricKindReconciliation, metrics.MetricPhaseDelete, func(phase string) (struct{}, error) {
		err := c.portClient.DeleteStaleEntities(ctx, c.stateKey, goutils.MergeMaps(currentEntitiesSet...))
		if err != nil {
			logger.Errorf("error deleting stale entities: %s", err.Error())
		}
		return struct{}{}, nil
	})
}

func (c *ControllersHandler) Stop() {
	if c.isStopped {
		return
	}

	logger.Info("Stopping controllers")
	close(c.stopCh)
	c.isStopped = true
}
