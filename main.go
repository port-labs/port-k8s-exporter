package main

import (
	"flag"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"os"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/event_listener"
	"github.com/port-labs/port-k8s-exporter/pkg/handlers"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
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

func InitiateHandler(exporterConfig *port.Config, k8sClient *k8s.Client, portClient *cli.PortClient) (*handlers.ControllersHandler, error) {
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

	exporterConfig, err := config.New(configFilePath, resyncInterval, stateKey, eventListenerType)
	if err != nil {
		klog.Fatalf("Error building Port K8s Exporter config: %s", err.Error())
	}

	_, err = integration.GetIntegrationConfig(portClient, stateKey)
	if err != nil {
		err = integration.NewIntegration(portClient, stateKey, exporterConfig)
		if err != nil {
			klog.Fatalf("Error creating K8s integration: %s", err.Error())
		}
	}
	cli.WithHeader("User-Agent", fmt.Sprintf("port-k8s-exporter/0.1 (statekey/%s)", exporterConfig.StateKey))(portClient)

	klog.Info("Starting controllers handler")
	handler, _ := InitiateHandler(exporterConfig, k8sClient, portClient)
	eventListener := event_listener.NewEventListener(stateKey, eventListenerType, handler, portClient)
	err = eventListener.Start(func(handler *handlers.ControllersHandler) (*handlers.ControllersHandler, error) {
		handler.Stop()
		return InitiateHandler(exporterConfig, k8sClient, portClient)
	})
	if err != nil {
		klog.Fatalf("Error starting event listener: %s", err.Error())
	}
	klog.Info("Started controllers handler")
}

func init() {
	flag.StringVar(&configFilePath, "config", "", "Path to Port K8s Exporter config file. Required.")
	flag.StringVar(&stateKey, "state-key", "", "Port K8s Exporter state key id. Required.")
	flag.BoolVar(&deleteDependents, "delete-dependents", false, "Flag to enable deletion of dependent Port Entities. Optional.")
	flag.BoolVar(&createMissingRelatedEntities, "create-missing-related-entities", false, "Flag to enable creation of missing related Port entities. Optional.")
	flag.UintVar(&resyncInterval, "resync-interval", 0, "The re-sync interval in minutes. Optional.")
	flag.StringVar(&portBaseURL, "port-base-url", "https://api.getport.io", "Port base URL. Optional.")
	portClientId = os.Getenv("PORT_CLIENT_ID")
	portClientSecret = os.Getenv("PORT_CLIENT_SECRET")

	eventListenerType = goutils.GetEnvOrDefault("EVENT_LISTENER__TYPE", "POLLING")
}
