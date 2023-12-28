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
	klog.Infof("Initializing Port integration")
	existingIntegration, err := integration.GetIntegration(portClient, applicationConfig.StateKey)
	defaultIntegrationConfig := &port.IntegrationAppConfig{
		Resources:                    applicationConfig.Resources,
		DeleteDependents:             applicationConfig.DeleteDependents,
		CreateMissingRelatedEntities: applicationConfig.CreateMissingRelatedEntities,
	}

	if err != nil {
		klog.Infof("Could not get integration with state key %s, error: %s", applicationConfig.StateKey, err.Error())
		klog.Infof("Creating integration")
		// The exporter supports a deprecated case where resources are provided in config file and integration does not
		// exist. If this is not the case, we support the new way of creating the integration with the default resources.
		// Only one of the two cases can be true.
		if defaultIntegrationConfig.Resources == nil && applicationConfig.CreateDefaultResources {
			klog.Infof("Creating default resources")
			if err := initializeDefaults(portClient, applicationConfig); err != nil {
				klog.Warningf("Error initializing defaults: %s", err.Error())
				klog.Warningf("The integration will start without default integration mapping and other default resources. Please create them manually in Port. ")
			} else {
				klog.Infof("Default resources created successfully")
				return nil
			}
		}

		klog.Infof("Could not create default resources, creating integration with no resources")
		klog.Infof("Creating integration with config: %v", defaultIntegrationConfig)
		// Handle a deprecated case where resources are provided in config file
		return integration.CreateIntegration(portClient, applicationConfig.StateKey, applicationConfig.EventListenerType, defaultIntegrationConfig)
	} else {
		klog.Infof("integration : %v", existingIntegration)
		klog.Infof("Integration with state key %s already exists, patching it", applicationConfig.StateKey)
		integrationPatch := &port.Integration{
			EventListener: getEventListenerConfig(applicationConfig.EventListenerType),
		}

		// Handle a deprecated case where resources are provided in config file and integration exists from previous
		//versions without a config
		if existingIntegration.Config == nil {
			klog.Infof("Integration exists without a config, patching it with default config: %v", defaultIntegrationConfig)
			integrationPatch.Config = defaultIntegrationConfig
		}

		return integration.PatchIntegration(portClient, applicationConfig.StateKey, integrationPatch)
	}
}
