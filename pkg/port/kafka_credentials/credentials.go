package kafka_credentials

import (
	"context"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"k8s.io/klog/v2"
)

func GetKafkaCredentials(portClient *cli.PortClient) (*port.OrgKafkaCredentials, error) {
	klog.Infof("Getting Port kafka credentials")
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("error authenticating with Port: %v", err)
	}

	r, err := portClient.GetKafkaCredentials()
	if err != nil {
		return nil, fmt.Errorf("error getting Port org credentials: %v", err)
	}

	if r.Username == "" || r.Password == "" {
		return nil, fmt.Errorf("error getting Port org credentials: username or password is empty")
	}

	return r, nil
}
