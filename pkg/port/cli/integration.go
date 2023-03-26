package cli

import (
	"encoding/json"
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

func (c *PortClient) ReadIntegration(id string) (*port.Integration, error) {
	resp, err := c.Client.R().
		SetHeader("Accept", "application/json").
		SetPathParam("identifier", id).
		Get("v1/integration/{identifier}")
	if err != nil {
		return nil, err
	}
	var pb port.ResponseBody
	err = json.Unmarshal(resp.Body(), &pb)
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to read integration, got: %s", resp.Body())
	}
	return &pb.Integration, nil
}

func (c *PortClient) CreateIntegration(integration *port.Integration) (*port.Integration, error) {
	resp, err := c.Client.R().
		SetHeader("Accept", "application/json").
		SetBody(integration).
		Post("v1/integration")
	if err != nil {
		return nil, err
	}
	var pb port.ResponseBody
	err = json.Unmarshal(resp.Body(), &pb)
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to create entity, got: %s", resp.Body())
	}
	return &pb.Integration, nil
}
