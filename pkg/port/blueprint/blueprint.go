package blueprint

import (
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func NewBlueprint(portClient *cli.PortClient, blueprint port.Blueprint) (*port.Blueprint, error) {
	return InnerNewBlueprint(portClient, blueprint, true)
}

func NewBlueprintWithoutPage(portClient *cli.PortClient, blueprint port.Blueprint) (*port.Blueprint, error) {
	return InnerNewBlueprint(portClient, blueprint, false)
}

func InnerNewBlueprint(portClient *cli.PortClient, blueprint port.Blueprint, shouldCreatePage bool) (*port.Blueprint, error) {
	var err error

	var bp *port.Blueprint
	if shouldCreatePage {
		bp, err = cli.CreateBlueprint(portClient, blueprint)
	} else {
		bp, err = cli.CreateBlueprintWithoutPage(portClient, blueprint)
	}

	if err != nil {
		return nil, fmt.Errorf("error creating blueprint: %v", err)
	}
	return bp, nil
}

func PatchBlueprint(portClient *cli.PortClient, blueprint port.Blueprint) (*port.Blueprint, error) {
	bp, err := cli.PatchBlueprint(portClient, blueprint)
	if err != nil {
		return nil, fmt.Errorf("error patching Port blueprint: %v", err)
	}
	return bp, nil
}

func DeleteBlueprint(portClient *cli.PortClient, blueprintIdentifier string) error {
	err := cli.DeleteBlueprint(portClient, blueprintIdentifier)
	if err != nil {
		return fmt.Errorf("error deleting Port blueprint: %v", err)
	}
	return nil
}

func DeleteBlueprintEntities(portClient *cli.PortClient, blueprintIdentifier string) error {
	err := cli.DeleteBlueprintEntities(portClient, blueprintIdentifier)
	if err != nil {
		return fmt.Errorf("error deleting Port blueprint entities: %v", err)
	}
	return nil
}

func GetBlueprint(portClient *cli.PortClient, blueprintIdentifier string) (*port.Blueprint, error) {
	bp, err := cli.GetBlueprint(portClient, blueprintIdentifier)
	if err != nil {
		return nil, fmt.Errorf("error getting Port blueprint: %v", err)
	}
	return bp, nil
}
