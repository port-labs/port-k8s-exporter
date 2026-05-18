package cli

import (
	"encoding/json"
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
	return c.getIntegration(stateKey, nil)
}

func (c *PortClient) GetIntegrationForPolling(stateKey string) (*port.Integration, error) {
	return c.getIntegration(stateKey, map[string]string{
		"isPolling": "true",
	})
}

func (c *PortClient) getIntegration(stateKey string, queryParams map[string]string) (*port.Integration, error) {
	pb := &port.ResponseBody{}
	req := c.Client.R().SetResult(&pb)
	if len(queryParams) > 0 {
		req.SetQueryParams(queryParams)
	}
	resp, err := req.Get(fmt.Sprintf("v1/integration/%s", stateKey))
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to get integration, got: %s", resp.Body())
	}
	return &pb.Integration, nil
}

type integrationResyncRequestResponse struct {
	OK      bool                                 `json:"ok"`
	Request *port.IntegrationResyncTriggerRequest `json:"request"`
}

func (c *PortClient) GetIntegrationResyncRequest(stateKey string) (*port.IntegrationResyncTriggerRequest, error) {
	pb := &integrationResyncRequestResponse{}
	resp, err := c.Client.R().
		SetResult(&pb).
		Get(fmt.Sprintf("v1/integration/%s/resync-request", stateKey))
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to get integration resync request, got: %s", resp.Body())
	}
	return pb.Request, nil
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
	jsonBytes, err := json.Marshal(examples)
	if err != nil {
		return fmt.Errorf("failed to marshal examples: %w", err)
	}

	// Deserialize into public interface{} structure
	var jsonData interface{}
	if err := json.Unmarshal(jsonBytes, &jsonData); err != nil {
		return fmt.Errorf("failed to unmarshal examples: %w", err)
	}

	maskedExamples := parsers.ParseSensitiveData(jsonData)

	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetBody(map[string]interface{}{
			"examples": maskedExamples,
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
