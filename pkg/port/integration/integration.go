package integration

import (
	"context"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func CreateIntegration(portClient *cli.PortClient, stateKey string, eventListenerType string, appConfig *port.IntegrationAppConfig) error {
	integration := &port.Integration{
		Title:               stateKey,
		InstallationAppType: "K8S EXPORTER",
		InstallationId:      stateKey,
		EventListener: &port.EventListenerSettings{
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

	if apiIntegration.Config != nil {
		defaultTrue := true
		if apiIntegration.Config.SendRawDataExamples == nil {
			apiIntegration.Config.SendRawDataExamples = &defaultTrue
		}
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

func PostIntegrationKindExample(portClient *cli.PortClient, stateKey string, kind string, examples []interface{}) error {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return fmt.Errorf("error authenticating with Port: %v", err)
	}

	err = portClient.PostIntegrationKindExample(stateKey, kind, examples)
	if err != nil {
		return err
	}
	return nil
}
