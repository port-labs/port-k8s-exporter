package page

import (
	"context"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func CreatePage(portClient *cli.PortClient, page port.Page) error {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return fmt.Errorf("error authenticating with Port: %v", err)
	}

	err = portClient.CreatePage(page)
	if err != nil {
		return fmt.Errorf("error creating Port page: %v", err)
	}
	return nil
}

func GetPage(portClient *cli.PortClient, identifier string) (*port.Page, error) {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("error authenticating with Port: %v", err)
	}

	apiPage, err := portClient.GetPage(identifier)
	if err != nil {
		return nil, fmt.Errorf("error getting Port page: %v", err)
	}

	return apiPage, nil
}

func DeletePage(portClient *cli.PortClient, identifier string) error {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return fmt.Errorf("error authenticating with Port: %v", err)
	}

	err = portClient.DeletePage(identifier)
	if err != nil {
		return fmt.Errorf("error deleting Port page: %v", err)
	}
	return nil
}
