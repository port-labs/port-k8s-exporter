package cli

import (
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

func (c *PortClient) CreateIntegration(i *port.Integration) (*port.Integration, error) {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetBody(i).
		SetResult(&pb).
		SetQueryParam("upsert", "true").
		Post("v1/integration")
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to create integration, got: %s", resp.Body())
	}
	return &pb.Integration, nil
}
