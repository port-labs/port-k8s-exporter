package cli

import (
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

func (c *PortClient) GetKafkaCredentials() (*port.OrgKafkaCredentials, error) {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetResult(&pb).
		Get("v1/kafka-credentials")
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to get kafka crednetials, got: %s", resp.Body())
	}
	return &pb.KafkaCredentials, nil
}
