package integration

import (
	"context"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func NewIntegration(portClient *cli.PortClient, stateKey string, eventListenerType string, resources []port.Resource) error {
	integration := &port.Integration{
		Title:               stateKey,
		InstallationAppType: "K8S EXPORTER",
		InstallationId:      stateKey,
		EventListener: port.EventListenerSettings{
			Type: eventListenerType,
		},
		Config: &port.AppConfig{
			Resources: resources,
		},
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

// ToDo: remove this function
func GetIntegrationConfig(portClient *cli.PortClient, stateKey string) (*port.AppConfig, error) {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("error authenticating with Port: %v", err)
	}

	apiConfig, err := portClient.GetIntegrationConfig(stateKey)
	if err != nil {
		return nil, fmt.Errorf("error getting Port integration config: %v", err)
	}

	return apiConfig, nil
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

func UpdateIntegrationConfig(portClient *cli.PortClient, stateKey string, config *port.AppConfig) error {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return fmt.Errorf("error authenticating with Port: %v", err)
	}

	err = portClient.UpdateConfig(stateKey, config)
	if err != nil {
		return fmt.Errorf("error updating Port integration config: %v", err)
	}
	return nil
}
