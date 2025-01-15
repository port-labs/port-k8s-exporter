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
	defaults, err := getDefaults()
	if err != nil {
		return err
	}
	
	existingIntegration, err := integration.GetIntegration(portClient, applicationConfig.StateKey)
	defaultIntegrationConfig := &port.IntegrationAppConfig{
		Resources:                    applicationConfig.Resources,
		CRDSToDiscover:               applicationConfig.CRDSToDiscover,
		OverwriteCRDsActions:         applicationConfig.OverwriteCRDsActions,
		DeleteDependents:             applicationConfig.DeleteDependents,
		CreateMissingRelatedEntities: applicationConfig.CreateMissingRelatedEntities,
	}

	if err != nil {
		if applicationConfig.CreateDefaultResources {
			defaultIntegrationConfig = defaults.AppConfig
		}

		klog.Infof("Could not get integration with state key %s, error: %s", applicationConfig.StateKey, err.Error())
		if err := integration.CreateIntegration(portClient, applicationConfig.StateKey, applicationConfig.EventListenerType, defaultIntegrationConfig); err != nil {
			return err
		}
	} else {
		klog.Infof("Integration with state key %s already exists, patching it", applicationConfig.StateKey)
		integrationPatch := &port.Integration{
			EventListener: getEventListenerConfig(applicationConfig.EventListenerType),
		}

		if existingIntegration.Config == nil || applicationConfig.OverwriteConfigurationOnRestart {
			integrationPatch.Config = defaultIntegrationConfig
		}

		if err := integration.PatchIntegration(portClient, applicationConfig.StateKey, integrationPatch); err != nil {
			return err
		}
	}

	if applicationConfig.CreateDefaultResources {
		klog.Infof("Creating default resources")
		if err := initializeDefaults(portClient, defaults); err != nil {
			klog.Warningf("Error initializing defaults: %s", err.Error())
			klog.Warningf("Some default resources may not have been created. The integration will continue running.")
		}
	}

	return nil
}
