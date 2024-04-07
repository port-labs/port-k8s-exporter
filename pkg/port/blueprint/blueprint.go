package blueprint

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/klog/v2"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func NewBlueprint(portClient *cli.PortClient, blueprint port.Blueprint, upsert bool) (*port.Blueprint, error) {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)

	if err != nil {
		return nil, fmt.Errorf("error authenticating with Port: %v", err)
	}

	bp, err := cli.CreateBlueprint(portClient, blueprint)
	if err != nil {
		if upsert {
			if strings.Contains(err.Error(), "taken") {
				klog.Infof("Blueprint already exists, patching blueprint")
				return PatchBlueprint(portClient, blueprint)
			}
		}
		return nil, fmt.Errorf("error creating blueprint: %v", err)
	}
	return bp, nil
}

func NewBlueprintAction(portClient *cli.PortClient, blueprintIdentifier string, action port.Action) (*port.Action, error) {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("error authenticating with Port: %v", err)
	}
	act, err := cli.CreateAction(portClient, blueprintIdentifier, action)
	if err != nil {
		return nil, fmt.Errorf("error creating blueprint action: %v", err)
	}
	return act, nil
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
