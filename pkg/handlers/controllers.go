package handlers

import (
	"context"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/klog/v2"
	"time"
)

type ControllersHandler struct {
	controllers      []*k8s.Controller
	informersFactory dynamicinformer.DynamicSharedInformerFactory
	exporterConfig   *config.Config
	portClient       *cli.PortClient
}

func NewControllersHandler(exporterConfig *config.Config, k8sClient *k8s.Client, portClient *cli.PortClient) *ControllersHandler {
	resync := time.Minute * time.Duration(exporterConfig.ResyncInterval)
	informersFactory := dynamicinformer.NewDynamicSharedInformerFactory(k8sClient.DynamicClient, resync)
	controllers := make([]*k8s.Controller, 0, len(exporterConfig.Resources))

	for _, resource := range exporterConfig.Resources {
		var gvr schema.GroupVersionResource
		gvr, err := k8s.GetGVRFromResource(k8sClient.DiscoveryMapper, resource.Kind)
		if err != nil {
			klog.Errorf("Error getting GVR, skip handling for resource '%s': %s.", resource.Kind, err.Error())
			continue
		}

		informer := informersFactory.ForResource(gvr)
		controller := k8s.NewController(resource, portClient, informer)
		controllers = append(controllers, controller)
	}

	if len(controllers) == 0 {
		klog.Fatalf("Failed to initiate a controller for all resources, exiting...")
	}

	controllersHandler := &ControllersHandler{
		controllers:      controllers,
		informersFactory: informersFactory,
		exporterConfig:   exporterConfig,
		portClient:       portClient,
	}

	return controllersHandler
}

func (c *ControllersHandler) Handle(stopCh <-chan struct{}) {
	klog.Info("Starting informers")
	c.informersFactory.Start(stopCh)
	klog.Info("Starting controllers")
	for _, controller := range c.controllers {
		if err := controller.Run(1, stopCh); err != nil {
			klog.Fatalf("Error running controller: %s", err.Error())
		}
	}
	klog.Info("Deleting stale entities")
	go c.RunDeleteStaleEntities()

	<-stopCh
	klog.Info("Shutting down controllers")
	for _, controller := range c.controllers {
		controller.Shutdown()
	}
	klog.Info("Exporter exiting")
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

	err = c.portClient.DeleteStaleEntities(context.Background(), c.exporterConfig.StateKey, goutils.MergeMaps(currentEntitiesSet...))
	if err != nil {
		klog.Errorf("error deleting stale entities: %s", err.Error())
	}
	klog.Info("Done deleting stale entities")
}
