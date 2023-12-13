package integration

import (
	"context"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func NewIntegration(portClient *cli.PortClient, exporterConfig *port.Config, resources []port.Resource) error {
	integration := &port.Integration{
		Title:               exporterConfig.StateKey,
		InstallationAppType: "K8S EXPORTER",
		InstallationId:      exporterConfig.StateKey,
		EventListener: port.EventListenerSettings{
			Type: exporterConfig.EventListenerType,
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
