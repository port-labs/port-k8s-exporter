package kafka_credentials

import (
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func GetKafkaCredentials(portClient *cli.PortClient) (*port.OrgKafkaCredentials, error) {
	logger.Infof("Getting Port kafka credentials")
	r, err := portClient.GetKafkaCredentials()
	if err != nil {
		return nil, fmt.Errorf("error getting Port org credentials: %v", err)
	}

	if r.Username == "" || r.Password == "" {
		return nil, fmt.Errorf("error getting Port org credentials: username or password is empty")
	}

	return r, nil
}
