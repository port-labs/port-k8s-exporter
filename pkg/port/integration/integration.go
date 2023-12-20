package integration

import (
	"context"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/defaults"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"k8s.io/klog/v2"
)

func NewIntegration(portClient *cli.PortClient, stateKey string, eventListenerType string, appConfig *port.IntegrationConfig) error {
	integration := &port.Integration{
		Title:               stateKey,
		InstallationAppType: "K8S EXPORTER",
		InstallationId:      stateKey,
		EventListener: port.EventListenerSettings{
			Type: eventListenerType,
		},
		Config: appConfig,
	}
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return fmt.Errorf("error authenticating with Port: %v", err)
	}

	_, err = portClient.CreateIntegration(integration)
	if err != nil {
		return fmt.Errorf("error creating Port integration: %v", err)
	}
	return nil
}

func GetIntegration(portClient *cli.PortClient, stateKey string) (*port.Integration, error) {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("error authenticating with Port: %v", err)
	}

	apiIntegration, err := portClient.GetIntegration(stateKey)
	if err != nil {
		return nil, fmt.Errorf("error getting Port integration: %v", err)
	}

	return apiIntegration, nil
}

func DeleteIntegration(portClient *cli.PortClient, stateKey string) error {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return fmt.Errorf("error authenticating with Port: %v", err)
	}

	err = portClient.DeleteIntegration(stateKey)
	if err != nil {
		return fmt.Errorf("error deleting Port integration: %v", err)
	}
	return nil
}

func PatchIntegration(portClient *cli.PortClient, stateKey string, integration *port.Integration) error {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return fmt.Errorf("error authenticating with Port: %v", err)
	}

	err = portClient.PatchIntegration(stateKey, integration)
	if err != nil {
		return fmt.Errorf("error updating Port integration: %v", err)
	}
	return nil
}

func InitIntegration(portClient *cli.PortClient, applicationConfig *port.Config) error {
	existingIntegration, err := GetIntegration(portClient, applicationConfig.StateKey)
	defaultIntegrationConfig := &port.IntegrationConfig{}

	if applicationConfig.Resources != nil {
		defaultIntegrationConfig.Resources = applicationConfig.Resources
	}

	if err != nil {
		var defaultsInitializationError error
		if applicationConfig.Resources == nil {
			defaultsInitializationError = defaults.InitializeDefaults(portClient, applicationConfig)
			if err != nil {
				klog.Warningf("Error initializing defaults: %s", err.Error())
			}
		}
		if defaultsInitializationError != nil || applicationConfig.Resources != nil {
			// Handle a deprecated case where resources are provided in config file
			err = NewIntegration(portClient, applicationConfig.StateKey, applicationConfig.EventListenerType, defaultIntegrationConfig)
			if err != nil {
				return fmt.Errorf("error creating Port integration: %v", err)
			}
		}
	} else {
		integrationPatch := &port.Integration{
			EventListener: port.EventListenerSettings{
				Type: applicationConfig.EventListenerType,
			},
		}
		if existingIntegration.Config == nil {
			integrationPatch.Config = defaultIntegrationConfig
		}

		if err := PatchIntegration(portClient, applicationConfig.StateKey, integrationPatch); err != nil {
			return fmt.Errorf("error updating Port integration: %v", err)
		}
	}

	return nil
}
