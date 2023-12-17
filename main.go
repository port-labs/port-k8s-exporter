package main

import (
	"flag"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/event_listener"
	"github.com/port-labs/port-k8s-exporter/pkg/handlers"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"k8s.io/klog/v2"
)

func initiateHandler(exporterConfig *port.Config, k8sClient *k8s.Client, portClient *cli.PortClient) (*handlers.ControllersHandler, error) {
	apiConfig, err := integration.GetIntegrationConfig(portClient, config.ApplicationConfig.StateKey)
	if err != nil {
		klog.Fatalf("Error getting K8s integration config: %s", err.Error())
	}

	cli.WithDeleteDependents(apiConfig.DeleteDependents)(portClient)
	cli.WithCreateMissingRelatedEntities(apiConfig.CreateMissingRelatedEntities)(portClient)

	newHandler := handlers.NewControllersHandler(exporterConfig, apiConfig, k8sClient, portClient)
	newHandler.Handle()

	return newHandler, nil
}

func main() {
	klog.InitFlags(nil)

	k8sConfig := k8s.NewKubeConfig()

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
		cli.WithHeader("User-Agent", fmt.Sprintf("port-k8s-exporter/0.1 (statekey/%s)", config.ApplicationConfig.StateKey)),
	)

	if err != nil {
		klog.Fatalf("Error building Port client: %s", err.Error())
	}

	exporterConfig, _ := config.GetConfigFile(config.ApplicationConfig.ConfigFilePath, config.ApplicationConfig.ResyncInterval, config.ApplicationConfig.StateKey, config.ApplicationConfig.EventListenerType)

	_, err = integration.GetIntegrationConfig(portClient, config.ApplicationConfig.StateKey)
	if err != nil {
		if exporterConfig == nil {
			klog.Fatalf("The integration does not exist and no config file was provided")
		}
		err = integration.NewIntegration(portClient, exporterConfig, exporterConfig.Resources)
		if err != nil {
			klog.Fatalf("Error creating K8s integration: %s", err.Error())
		}
	}

	klog.Info("Starting controllers handler")
	handler, _ := initiateHandler(exporterConfig, k8sClient, portClient)
	eventListener := event_listener.NewEventListener(config.ApplicationConfig.StateKey, config.ApplicationConfig.EventListenerType, handler, portClient)
	err = eventListener.Start(func(handler *handlers.ControllersHandler) (*handlers.ControllersHandler, error) {
		handler.Stop()
		return initiateHandler(exporterConfig, k8sClient, portClient)
	})
	if err != nil {
		klog.Fatalf("Error starting event listener: %s", err.Error())
	}
}

func init() {
	config.Init()
	flag.Parse()
}
