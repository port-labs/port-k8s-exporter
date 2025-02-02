package org_details

import (
	"context"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func GetOrgId(portClient *cli.PortClient) (string, error) {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return "", fmt.Errorf("error authenticating with Port: %v", err)
	}

	r, err := portClient.GetOrgId()
	if err != nil {
		return "", fmt.Errorf("error getting Port org credentials: %v", err)
	}

	return r, nil
}

func GetOrganizationFeatureFlags(portClient *cli.PortClient) ([]string, error) {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("error authenticating with Port: %v", err)
	}

	flags, err := portClient.GetOrganizationFeatureFlags()
	if err != nil {
		return nil, fmt.Errorf("error getting organization feature flags: %v", err)
	}

	return flags, nil
}
