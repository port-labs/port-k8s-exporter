package k8s

import (
	"context"
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"k8s.io/client-go/tools/clientcmd"
)

func NewIntegration(portClient *cli.PortClient, k8sConfig clientcmd.ClientConfig, stateKey string) error {

	k8sRawConfig, err := k8sConfig.RawConfig()
	if err != nil {
		return err
	}

	clusterName := k8sRawConfig.Contexts[k8sRawConfig.CurrentContext].Cluster

	integration := &port.Integration{
		Title:               clusterName,
		InstallationAppType: "kubernetes",
		InstallationId:      stateKey,
	}
	_, err = portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return fmt.Errorf("error authenticating with Port: %v", err)
	}

	_, err = portClient.CreateIntegration(integration)
	if err != nil {
		return fmt.Errorf("error creating Port integration: %v", err)
	}
	return nil
}
