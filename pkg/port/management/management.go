package management

import (
	"context"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"k8s.io/klog/v2"
)

func GetOrCreateIntegration(stateKey string, portClient *cli.PortClient) (*port.Integration, error) {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		klog.Errorf("error authenticating with Port: %v", err)
	}

	integration, err := portClient.ReadIntegration(stateKey)

	if err != nil {
		klog.Info("Integration not found, creating new one")
		integrationToCreate := new(port.Integration)
		integrationToCreate.InstallationId = stateKey
		integrationToCreate.InstallationAppType = "K8s"
		integration, err = portClient.CreateIntegration(integrationToCreate)
		if err != nil {
			klog.Fatalf("Error creating integration: %s", err.Error())
		}
	}

	klog.Info("Integration found with id: %s", integration.Id)
	return integration, err
}
