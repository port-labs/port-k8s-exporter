package k8s

import (
	"context"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

func NewIntegration(portClient *cli.PortClient, k8sConfig clientcmd.ClientConfig, stateKey string) {

	k8sRawConfig, err := k8sConfig.RawConfig()
	if err != nil {
		klog.Fatalf("Error getting K8s raw config: %s", err.Error())
	}

	clusterName := k8sRawConfig.Contexts[k8sRawConfig.CurrentContext].Cluster

	integration := &port.Integration{
		Title:               clusterName,
		InstallationAppType: "kubernetes",
		InstallationId:      stateKey,
	}
	_, err = portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		klog.Errorf("error authenticating with Port: %v", err)
	}

	_, err = portClient.CreateIntegration(integration)
	if err != nil {
		klog.Fatalf("Error creating Port integration: %s", err.Error())
	}
}
