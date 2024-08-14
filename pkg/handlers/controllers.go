package handlers

import (
	"context"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"sync"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/crd"
	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/signal"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/klog/v2"
)

type ControllersHandler struct {
	controllers      []*k8s.Controller
	informersFactory dynamicinformer.DynamicSharedInformerFactory
	stateKey         string
	portClient       *cli.PortClient
	stopCh           chan struct{}
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
			klog.Errorf("Error getting GVR, skip handling for resource '%s': %s.", kind, err.Error())
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
	}

	return controllersHandler
}

func (c *ControllersHandler) Handle() {
	klog.Info("Starting informers")
	c.informersFactory.Start(c.stopCh)

	currentEntitiesSets := make([]map[string]interface{}, 0)
	shouldDeleteStaleEntities := true
	var syncWg sync.WaitGroup

	for _, controller := range c.controllers {
		controller := controller

		go func() {
			<-c.stopCh
			klog.Info("Shutting down controllers")
			controller.Shutdown()
			klog.Info("Exporter exiting")
		}()

		klog.Infof("Waiting for informer cache to sync for resource '%s'", controller.Resource.Kind)
		if err := controller.WaitForCacheSync(c.stopCh); err != nil {
			klog.Fatalf("Error while waiting for informer cache sync: %s", err.Error())
		}

		syncWg.Add(1)
		go func() {
			defer syncWg.Done()
			klog.Infof("Starting full initial resync for resource '%s'", controller.Resource.Kind)
			initialSyncResult := controller.RunInitialSync()
			klog.Infof("Done full initial resync, starting live events sync for resource '%s'", controller.Resource.Kind)
			controller.RunEventsSync(1, c.stopCh)
			if initialSyncResult.EntitiesSet != nil {
				currentEntitiesSets = append(currentEntitiesSets, initialSyncResult.EntitiesSet)
			}
			if len(initialSyncResult.RawDataExamples) > 0 {
				err := integration.PostIntegrationKindExample(c.portClient, c.stateKey, controller.Resource.Kind, initialSyncResult.RawDataExamples)
				if err != nil {
					klog.Warningf("failed to post integration kind example: %s", err.Error())
				}
			}
			shouldDeleteStaleEntities = shouldDeleteStaleEntities && initialSyncResult.ShouldDeleteStaleEntities
		}()
	}
	syncWg.Wait()

	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx()
	go func() {
		<-c.stopCh
		cancelCtx()
	}()

	if shouldDeleteStaleEntities {
		klog.Info("Deleting stale entities")
		c.RunDeleteStaleEntities(ctx, currentEntitiesSets)
		klog.Info("Done deleting stale entities")
	} else {
		klog.Warning("Skipping delete of stale entities due to a failure in getting all current entities from k8s")
	}
}

func (c *ControllersHandler) RunDeleteStaleEntities(ctx context.Context, currentEntitiesSet []map[string]interface{}) {
	_, err := c.portClient.Authenticate(ctx, c.portClient.ClientID, c.portClient.ClientSecret)
	if err != nil {
		klog.Errorf("error authenticating with Port: %v", err)
	}

	err = c.portClient.DeleteStaleEntities(ctx, c.stateKey, goutils.MergeMaps(currentEntitiesSet...))
	if err != nil {
		klog.Errorf("error deleting stale entities: %s", err.Error())
	}
}

func (c *ControllersHandler) Stop() {
	klog.Info("Stopping controllers")
	close(c.stopCh)
}
