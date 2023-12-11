package integration

import (
	"context"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func NewIntegration(portClient *cli.PortClient, stateKey string, exporterConfig *port.Config) error {

	integration := &port.Integration{
		Title:               stateKey,
		InstallationAppType: "kubernetes",
		InstallationId:      stateKey,
		EventListener: port.EventListenerSettings{
			Type: exporterConfig.EventListenerType,
		},
		Config: &port.AppConfig{
			Resources: exporterConfig.Resources,
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
