package page

import (
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func CreatePage(portClient *cli.PortClient, page port.Page) error {
	err := portClient.CreatePage(page)
	if err != nil {
		return fmt.Errorf("error creating Port page: %v", err)
	}
	return nil
}

func GetPage(portClient *cli.PortClient, identifier string) (*port.Page, error) {
	apiPage, err := portClient.GetPage(identifier)
	if err != nil {
		return nil, fmt.Errorf("error getting Port page: %v", err)
	}

	return apiPage, nil
}

func DeletePage(portClient *cli.PortClient, identifier string) error {
	err := portClient.DeletePage(identifier)
	if err != nil {
		return fmt.Errorf("error deleting Port page: %v", err)
	}
	return nil
}
