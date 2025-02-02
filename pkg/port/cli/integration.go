package cli

import (
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/parsers"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"net/url"
)

func parseIntegration(i *port.Integration) *port.Integration {
	x := &port.Integration{
		Title:               i.Title,
		InstallationAppType: i.InstallationAppType,
		InstallationId:      i.InstallationId,
		Config:              i.Config,
	}

	if i.EventListener.Type == "KAFKA" {
		x.EventListener = &port.EventListenerSettings{
			Type: i.EventListener.Type,
		}
	}

	return x
}

func (c *PortClient) CreateIntegration(i *port.Integration, queryParams map[string]string) (*port.Integration, error) {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetQueryParams(queryParams).
		SetBody(parseIntegration(i)).
		SetResult(&pb).
		Post("v1/integration")
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to create integration, got: %s", resp.Body())
	}
	return &pb.Integration, nil
}

func (c *PortClient) GetIntegration(stateKey string) (*port.Integration, error) {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetResult(&pb).
		Get(fmt.Sprintf("v1/integration/%s", stateKey))
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to get integration, got: %s", resp.Body())
	}
	return &pb.Integration, nil
}

func (c *PortClient) DeleteIntegration(stateKey string) error {
	resp, err := c.Client.R().
		Delete(fmt.Sprintf("v1/integration/%s", stateKey))
	if err != nil {
		return err
	}
	if resp.StatusCode() != 200 {
		return fmt.Errorf("failed to delete integration, got: %s", resp.Body())
	}
	return nil
}

func (c *PortClient) PatchIntegration(stateKey string, integration *port.Integration) error {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetBody(integration).
		SetResult(&pb).
		Patch(fmt.Sprintf("v1/integration/%s", stateKey))
	if err != nil {
		return err
	}
	if !pb.OK {
		return fmt.Errorf("failed to update config, got: %s", resp.Body())
	}
	return nil
}

func (c *PortClient) PostIntegrationKindExample(stateKey string, kind string, examples []interface{}) error {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetBody(map[string]interface{}{
			"examples": parsers.ParseSensitiveData(examples),
		}).
		SetResult(&pb).
		Post(fmt.Sprintf("v1/integration/%s/kinds/%s/examples", url.QueryEscape(stateKey), url.QueryEscape(kind)))
	if err != nil {
		return err
	}
	if !pb.OK {
		return fmt.Errorf("failed to post integration kind example, got: %s", resp.Body())
	}
	return nil
}

func (c *PortClient) GetIntegrationKinds(stateKey string) (map[string]port.IntegrationKind, error) {
	pb := &port.IntegrationKindsResponse{}
	resp, err := c.Client.R().
		SetResult(&pb).
		Get(fmt.Sprintf("v1/integration/%s/kinds", stateKey))
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to get integration kinds, got: %s", resp.Body())
	}
	return pb.Data, nil
}
