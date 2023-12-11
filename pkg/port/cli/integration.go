package cli

import (
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

func parseIntegration(i *port.Integration) *port.Integration {
	x := &port.Integration{
		Title:               i.Title,
		InstallationAppType: i.InstallationAppType,
		InstallationId:      i.InstallationId,
		Config:              i.Config,
	}

	if i.EventListener.Type == "KAFKA" {
		x.EventListener = port.EventListenerSettings{
			Type: i.EventListener.Type,
		}
	}

	return x
}

func (c *PortClient) CreateIntegration(i *port.Integration) (*port.Integration, error) {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetBody(parseIntegration(i)).
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

func (c *PortClient) GetIntegrationConfig(stateKey string) (*port.AppConfig, error) {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetResult(&pb).
		Get(fmt.Sprintf("v1/integration/%s", stateKey))
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to get integration config, got: %s", resp.Body())
	}
	return pb.Integration.Config, nil
}
