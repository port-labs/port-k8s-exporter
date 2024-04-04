package main

import (
	"errors"
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/defaults"
	"github.com/port-labs/port-k8s-exporter/pkg/event_handler"
	"github.com/port-labs/port-k8s-exporter/pkg/handlers"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"k8s.io/klog/v2"
)

func initiateHandler(exporterConfig *port.Config, k8sClient *k8s.Client, portClient *cli.PortClient) (*handlers.ControllersHandler, error) {
	i, err := integration.GetIntegration(portClient, exporterConfig.StateKey)
	if err != nil {
		return nil, fmt.Errorf("error getting Port integration: %v", err)
	}
	if i.Config == nil {
		return nil, errors.New("integration config is nil")

	}

	cli.WithDeleteDependents(i.Config.DeleteDependents)(portClient)
	cli.WithCreateMissingRelatedEntities(i.Config.CreateMissingRelatedEntities)(portClient)

	newHandler := handlers.NewControllersHandler(exporterConfig, i.Config, k8sClient, portClient)
	newHandler.Handle()

	return newHandler, nil
}

func main() {
	k8sConfig := k8s.NewKubeConfig()
	applicationConfig, err := config.NewConfiguration()
	if err != nil {
		klog.Fatalf("Error getting application config: %s", err.Error())
	}

	clientConfig, err := k8sConfig.ClientConfig()
	if err != nil {
		klog.Fatalf("Error getting K8s client config: %s", err.Error())
	}

	k8sClient, err := k8s.NewClient(clientConfig)
	if err != nil {
		klog.Fatalf("Error building K8s client: %s", err.Error())
	}

	portClient, err := cli.New(config.ApplicationConfig.PortBaseURL,
		cli.WithClientID(config.ApplicationConfig.PortClientId), cli.WithClientSecret(config.ApplicationConfig.PortClientSecret),
		cli.WithHeader("User-Agent", fmt.Sprintf("port-k8s-exporter/0.1 (statekey/%s)", applicationConfig.StateKey)),
	)

	if err != nil {
		klog.Fatalf("Error building Port client: %s", err.Error())
	}

	if err := defaults.InitIntegration(portClient, applicationConfig); err != nil {
		klog.Fatalf("Error initializing Port integration: %s", err.Error())
	}

	eventListener, err := event_handler.CreateEventListener(applicationConfig.StateKey, applicationConfig.EventListenerType, portClient)
	if err != nil {
		klog.Fatalf("Error creating event listener: %s", err.Error())
	}

	klog.Info("Starting controllers handler")
	err = event_handler.Start(eventListener, func() (event_handler.IStoppableRsync, error) {
		return initiateHandler(applicationConfig, k8sClient, portClient)
	})

	if err != nil {
		klog.Fatalf("Error starting event listener: %s", err.Error())
	}
}

func init() {
	klog.InitFlags(nil)
	config.Init()
}
