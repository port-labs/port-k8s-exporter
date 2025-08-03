package main

import (
	"errors"
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/defaults"
	"github.com/port-labs/port-k8s-exporter/pkg/event_handler"
	"github.com/port-labs/port-k8s-exporter/pkg/handlers"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
)

func initiateHandler(exporterConfig *port.Config, k8sClient *k8s.Client, portClient *cli.PortClient) (*handlers.ControllersHandler, error) {
	i, err := integration.GetIntegration(portClient, exporterConfig.StateKey)
	if err != nil {
		return nil, fmt.Errorf("error getting Port integration: %v", err)
	}
	if i.Config == nil {
		return nil, errors.New("integration config is nil")

	}

	newHandler := handlers.NewControllersHandler(exporterConfig, i.Config, k8sClient, portClient)
	newHandler.Handle()

	return newHandler, nil
}

func main() {
	// Ensure logs are flushed before application exits
	defer logger.Shutdown()
	logger.Infow("Starting Port K8s Exporter", "version", Version)
	k8sConfig := k8s.NewKubeConfig()
	applicationConfig, err := config.NewConfiguration()
	if err != nil {
		logger.Fatalf("Error getting application config: %s", err.Error())
	}

	clientConfig, err := k8sConfig.ClientConfig()
	if err != nil {
		logger.Fatalf("Error getting K8s client config: %s", err.Error())
	}

	k8sClient, err := k8s.NewClient(clientConfig)
	if err != nil {
		logger.Fatalf("Error building K8s client: %s", err.Error())
	}
	portClient := cli.New(config.ApplicationConfig)

	if err := defaults.InitIntegration(portClient, applicationConfig, Version, false); err != nil {
		logger.Fatalf("Error initializing Port integration: %s", err.Error())
	}

	eventListener, err := event_handler.CreateEventListener(applicationConfig.StateKey, applicationConfig.EventListenerType, portClient)
	if err != nil {
		logger.Fatalf("Error creating event listener: %s", err.Error())
	}

	logger.Info("Starting controllers handler")
	err = event_handler.Start(eventListener, func() (event_handler.IStoppableRsync, error) {
		return initiateHandler(applicationConfig, k8sClient, portClient)
	})

	if err != nil {
		logger.Fatalf("Error starting event listener: %s", err.Error())
	}
}

func init() {
	config.Init()

	// Initialize logger with HTTP support if enabled
	if config.ApplicationConfig.HTTPLoggingEnabled {
		initHTTPLogger()
		return
	}
	if err := logger.Init(config.ApplicationConfig.LoggingLevel, config.ApplicationConfig.DebugMode); err != nil {
		panic(fmt.Sprintf("Failed to initialize logger: %v", err))
	}
}

func initHTTPLogger() {
	if err := logger.InitWithHTTP(config.ApplicationConfig.LoggingLevel, config.ApplicationConfig.DebugMode); err != nil {
		panic(fmt.Sprintf("Failed to initialize logger with HTTP: %v", err))
	}
}
