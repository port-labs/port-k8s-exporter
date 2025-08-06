package handlers

import (
	"context"
	"strings"
	"sync"
	"time"

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

func NewControllersHandler(exporterConfig *port.Config, portConfig *port.IntegrationAppConfig, k8sClient *k8s.Client, portClient *cli.PortClient) *ControllersHandler {
	resync := time.Minute * time.Duration(exporterConfig.ResyncInterval)
	informersFactory := dynamicinformer.NewDynamicSharedInformerFactory(k8sClient.DynamicClient, resync)

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

func (c *ControllersHandler) Handle() {
	logger.Info("Starting informers")
	c.informersFactory.Start(c.stopCh)

	currentEntitiesSets, shouldDeleteStaleEntities := syncAllControllers(c)

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	go func() {
		<-c.stopCh
		cancelCtx()
	}()

	if shouldDeleteStaleEntities {
		logger.Info("Deleting stale entities")
		c.runDeleteStaleEntities(ctx, currentEntitiesSets)
		logger.Info("Done deleting stale entities")
	} else {
		logger.Warning("Skipping delete of stale entities due to a failure in getting all current entities from k8s")
	}
}

func syncAllControllers(c *ControllersHandler) ([]map[string]interface{}, bool) {
	currentEntitiesSets := make([]map[string]interface{}, 0)
	shouldDeleteStaleEntities := true
	var syncWg sync.WaitGroup

	for _, controller := range c.controllers {
		go func() {
			<-c.stopCh
			logger.Info("Shutting down controllers")
			controller.Shutdown()
			// Flush any remaining logs before exit
			logger.Info("Exporter exiting")
			logger.Shutdown()
		}()

		logger.Infof("Waiting for informer cache to sync for resource '%s'", controller.Resource.Kind)
		if err := controller.WaitForCacheSync(c.stopCh); err != nil {
			logger.Fatalf("Error while waiting for informer cache sync: %s", err.Error())
		}

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
	return currentEntitiesSets, shouldDeleteStaleEntities
}

func syncController(controller *k8s.Controller, c *ControllersHandler) (map[string]interface{}, bool) {
	logger.Infof("Starting full initial resync for resource '%s'", controller.Resource.Kind)
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
	startDelete := time.Now()
	err := c.portClient.DeleteStaleEntities(ctx, c.stateKey, goutils.MergeMaps(currentEntitiesSet...))
	durationDelete := time.Since(startDelete).Seconds()
	// Assuming you can get kinds and counts from currentEntitiesSet
	for _, entities := range currentEntitiesSet {
		for kindKey, v := range entities {
			kind := kindKey
			if idx := strings.Index(kindKey, ";"); idx != -1 {
				kind = kindKey[:idx]
			}
			var count float64 = 0
			if arr, ok := v.([]interface{}); ok {
				count = float64(len(arr))
			}
			metrics.DurationSeconds.WithLabelValues(kind, metrics.MetricPhaseDelete).Set(durationDelete)
			metrics.ObjectCount.WithLabelValues(kind, "deleted", metrics.MetricPhaseDelete).Set(count)
		}
	}
	if err != nil {
		logger.Errorf("error deleting stale entities: %s", err.Error())
	}
}

func (c *ControllersHandler) Stop() {
	if c.isStopped {
		return
	}

	logger.Info("Stopping controllers")
	close(c.stopCh)
	c.isStopped = true
}
