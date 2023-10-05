package integration

import (
	"context"
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func NewIntegration(portClient *cli.PortClient, stateKey string) error {

	integration := &port.Integration{
		Title:               stateKey,
		InstallationAppType: "K8S EXPORTER",
		InstallationId:      stateKey,
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
