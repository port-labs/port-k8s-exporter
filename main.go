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

var (
	configFilePath               string
	resyncInterval               uint
	eventListenerType            string
	stateKey                     string
	deleteDependents             bool
	createMissingRelatedEntities bool
	portBaseURL                  string
	portClientId                 string
	portClientSecret             string
)

func initiateHandler(exporterConfig *port.Config, k8sClient *k8s.Client, portClient *cli.PortClient) (*handlers.ControllersHandler, error) {
	apiConfig, err := integration.GetIntegrationConfig(portClient, stateKey)
	if err != nil {
		klog.Fatalf("Error getting K8s integration config: %s", err.Error())
	}

	newHandler := handlers.NewControllersHandler(exporterConfig, apiConfig, k8sClient, portClient)
	newHandler.Handle()

	return newHandler, nil
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	k8sConfig := k8s.NewKubeConfig()

	clientConfig, err := k8sConfig.ClientConfig()
	if err != nil {
		klog.Fatalf("Error getting K8s client config: %s", err.Error())
	}

	k8sClient, err := k8s.NewClient(clientConfig)
	if err != nil {
		klog.Fatalf("Error building K8s client: %s", err.Error())
	}

	portClient, err := cli.New(portBaseURL,
		cli.WithClientID(portClientId), cli.WithClientSecret(portClientSecret),
		cli.WithDeleteDependents(deleteDependents), cli.WithCreateMissingRelatedEntities(createMissingRelatedEntities),
		cli.WithHeader("User-Agent", fmt.Sprintf("port-k8s-exporter/0.1 (statekey/%s)", stateKey)),
	)

	if err != nil {
		klog.Fatalf("Error building Port client: %s", err.Error())
	}

	exporterConfig, err := config.GetConfigFile(configFilePath, resyncInterval, stateKey, eventListenerType)
	if err != nil {
		klog.Fatalf("Error building Port K8s Exporter config: %s", err.Error())
	}

	_, err = integration.GetIntegrationConfig(portClient, stateKey)
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
	eventListener := event_listener.NewEventListener(stateKey, eventListenerType, handler, portClient)
	err = eventListener.Start(func(handler *handlers.ControllersHandler) (*handlers.ControllersHandler, error) {
		handler.Stop()
		return initiateHandler(exporterConfig, k8sClient, portClient)
	})
	if err != nil {
		klog.Fatalf("Error starting event listener: %s", err.Error())
	}
}

func init() {
	configFilePath = config.NewString("config", "", "Path to Port K8s Exporter config file. Required.")
	stateKey = config.NewString("state-key", "", "Port K8s Exporter state key id. Required.")

	// change to the app config
	//config.NewBoolean("delete-dependents", false, "Flag to enable deletion of dependent Port Entities. Optional.")
	//config.NewBoolean("create-missing-related-entities", false, "Flag to enable creation of missing related Port entities. Optional.")

	resyncInterval = config.NewUInt("resync-interval", 0, "The re-sync interval in minutes. Optional.")
	portBaseURL = config.NewString("port-base-url", "https://api.getport.io", "Port base URL. Optional.")

	portClientId = config.NewString("port-client-id", "", "Port client id. Required.")
	portClientSecret = config.NewString("port-client-secret", "", "Port client secret. Required.")

	config.NewString("event-listener-type", "POLLING", "Event listener type. Optional.")
}
