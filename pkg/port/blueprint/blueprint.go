package blueprint

import (
	"context"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func NewBlueprint(portClient *cli.PortClient, blueprint port.Blueprint) (*port.Blueprint, error) {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("error authenticating with Port: %v", err)
	}

	bp, err := cli.NewBlueprint(portClient, blueprint)
	if err != nil {
		return nil, fmt.Errorf("error creating Port blueprint: %v", err)
	}
	return bp, nil
}

func PatchBlueprint(portClient *cli.PortClient, blueprint port.Blueprint) (*port.Blueprint, error) {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("error authenticating with Port: %v", err)
	}

	bp, err := cli.PatchBlueprint(portClient, blueprint)
	if err != nil {
		return nil, fmt.Errorf("error patching Port blueprint: %v", err)
	}
	return bp, nil
}

func DeleteBlueprint(portClient *cli.PortClient, blueprintIdentifier string) error {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return fmt.Errorf("error authenticating with Port: %v", err)
	}

	err = cli.DeleteBlueprint(portClient, blueprintIdentifier)
	if err != nil {
		return fmt.Errorf("error deleting Port blueprint: %v", err)
	}
	return nil
}

func GetBlueprint(portClient *cli.PortClient, blueprintIdentifier string) (*port.Blueprint, error) {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("error authenticating with Port: %v", err)
	}

	bp, err := cli.GetBlueprint(portClient, blueprintIdentifier)
	if err != nil {
		return nil, fmt.Errorf("error getting Port blueprint: %v", err)
	}
	return bp, nil
}
