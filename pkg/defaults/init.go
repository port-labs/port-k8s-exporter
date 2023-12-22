package defaults

import (
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"k8s.io/klog/v2"
)

func getEventListenerConfig(eventListenerType string) *port.EventListenerSettings {
	if eventListenerType == "Kafka" {
		return &port.EventListenerSettings{
			Type: eventListenerType,
		}
	}
	return nil
}

func InitIntegration(portClient *cli.PortClient, applicationConfig *port.Config) error {
	existingIntegration, err := integration.GetIntegration(portClient, applicationConfig.StateKey)
	defaultIntegrationConfig := &port.IntegrationConfig{}

	if applicationConfig.Resources != nil {
		defaultIntegrationConfig.Resources = applicationConfig.Resources
	}

	if err != nil {
		var defaultsInitializationError error
		if applicationConfig.Resources == nil {
			defaultsInitializationError = initializeDefaults(portClient, applicationConfig)
			if err != nil {
				klog.Warningf("Error initializing defaults: %s", err.Error())
			}
		}
		if defaultsInitializationError != nil || applicationConfig.Resources != nil {
			// Handle a deprecated case where resources are provided in config file
			return integration.NewIntegration(portClient, applicationConfig.StateKey, applicationConfig.EventListenerType, defaultIntegrationConfig)
		}
	} else {
		// Handle a deprecated case where resources are provided in config file and integration already exists in port with no resources
		integrationPatch := &port.Integration{
			EventListener: getEventListenerConfig(applicationConfig.EventListenerType),
		}
		if existingIntegration.Config == nil {
			integrationPatch.Config = defaultIntegrationConfig
		}

		return integration.PatchIntegration(portClient, applicationConfig.StateKey, integrationPatch)
	}

	return nil
}
