package cli

import (
	"encoding/json"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

func (c *PortClient) GetOrgId() (string, error) {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetResult(&pb).
		Get("v1/organization")

	z := &struct {
	}{}
	_ = json.Unmarshal(resp.Body(), &z)
	if err != nil {
		return "", err
	}
	if !pb.OK {
		return "", fmt.Errorf("failed to get orgId, got: %s", resp.Body())
	}
	return pb.OrgDetails.OrgId, nil
}

func (c *PortClient) GetOrganizationFeatureFlags() ([]string, error) {
	pb := &port.ResponseBody{}
	resp, err := c.Client.R().
		SetResult(&pb).
		Get("v1/organization")
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to get organization feature flags, got: %s", resp.Body())
	}
	return pb.OrgDetails.FeatureFlags, nil
}
