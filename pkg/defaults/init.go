package defaults

import (
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"k8s.io/klog/v2"
)

func getEventListenerConfig(eventListenerType string) *port.EventListenerSettings {
	if eventListenerType == "KAFKA" {
		return &port.EventListenerSettings{
			Type: eventListenerType,
		}
	}
	return nil
}

func InitIntegration(portClient *cli.PortClient, applicationConfig *port.Config) error {
	existingIntegration, err := integration.GetIntegration(portClient, applicationConfig.StateKey)
	defaultIntegrationConfig := &port.IntegrationAppConfig{
		Resources: applicationConfig.Resources,
	}

	if err != nil {
		if defaultIntegrationConfig.Resources == nil && applicationConfig.CreateDefaultResources {
			if err := initializeDefaults(portClient, applicationConfig); err != nil {
				klog.Warningf("Error initializing defaults: %s", err.Error())
			} else {
				return nil
			}
		}

		// Handle a deprecated case where resources are provided in config file
		return integration.CreateIntegration(portClient, applicationConfig.StateKey, applicationConfig.EventListenerType, defaultIntegrationConfig)
	} else {
		integrationPatch := &port.Integration{
			EventListener: getEventListenerConfig(applicationConfig.EventListenerType),
		}

		// Handle a deprecated case where resources are provided in config file and integration exists from previous
		//versions without a config
		if existingIntegration.Config == nil && applicationConfig.Resources != nil {
			integrationPatch.Config = &port.IntegrationAppConfig{
				DeleteDependents:             defaultIntegrationConfig.DeleteDependents,
				CreateMissingRelatedEntities: defaultIntegrationConfig.CreateMissingRelatedEntities,
				Resources:                    defaultIntegrationConfig.Resources,
			}
		}

		return integration.PatchIntegration(portClient, applicationConfig.StateKey, integrationPatch)
	}
}
