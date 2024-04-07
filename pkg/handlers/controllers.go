package handlers

import (
	"context"
	"time"

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

	matchedCrds := crd.AutodiscoverCRDsToActions(exporterConfig, portConfig, k8sClient, portClient)

	aggResources := make(map[string][]port.KindConfig)
	for _, resource := range portConfig.Resources {
		kindConfig := port.KindConfig{Selector: resource.Selector, Port: resource.Port}
		if _, ok := aggResources[resource.Kind]; ok {
			aggResources[resource.Kind] = append(aggResources[resource.Kind], kindConfig)
		} else {
			aggResources[resource.Kind] = []port.KindConfig{kindConfig}
		}
	}

	controllers := make([]*k8s.Controller, 0, len(portConfig.Resources)+len(matchedCrds))

	for kind, kindConfigs := range aggResources {
		var gvr schema.GroupVersionResource
		gvr, err := k8s.GetGVRFromResource(k8sClient.DiscoveryMapper, kind)
		if err != nil {
			klog.Errorf("Error getting GVR, skip handling for resource '%s': %s.", kind, err.Error())
			continue
		}

		informer := informersFactory.ForResource(gvr)
		controller := k8s.NewController(port.AggregatedResource{Kind: kind, KindConfigs: kindConfigs}, portClient, informer)
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
	klog.Info("Waiting for informers cache sync")
	for _, controller := range c.controllers {
		if err := controller.WaitForCacheSync(c.stopCh); err != nil {
			klog.Fatalf("Error while waiting for informer cache sync: %s", err.Error())
		}
	}
	klog.Info("Deleting stale entities")
	c.RunDeleteStaleEntities()
	klog.Info("Starting controllers")
	for _, controller := range c.controllers {
		controller.Run(1, c.stopCh)
	}

	go func() {
		<-c.stopCh
		klog.Info("Shutting down controllers")
		for _, controller := range c.controllers {
			controller.Shutdown()
		}
		klog.Info("Exporter exiting")
	}()
}

func (c *ControllersHandler) RunDeleteStaleEntities() {
	currentEntitiesSet := make([]map[string]interface{}, 0)
	for _, controller := range c.controllers {
		controllerEntitiesSet, err := controller.GetEntitiesSet()
		if err != nil {
			klog.Errorf("error getting controller entities set: %s", err.Error())
		}
		currentEntitiesSet = append(currentEntitiesSet, controllerEntitiesSet)
	}

	_, err := c.portClient.Authenticate(context.Background(), c.portClient.ClientID, c.portClient.ClientSecret)
	if err != nil {
		klog.Errorf("error authenticating with Port: %v", err)
	}

	err = c.portClient.DeleteStaleEntities(context.Background(), c.stateKey, goutils.MergeMaps(currentEntitiesSet...))
	if err != nil {
		klog.Errorf("error deleting stale entities: %s", err.Error())
	}
	klog.Info("Done deleting stale entities")
}

func (c *ControllersHandler) Stop() {
	klog.Info("Stopping controllers")
	close(c.stopCh)
}
