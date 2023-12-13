package blueprint

import (
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func NewBlueprint(portClient *cli.PortClient, blueprint port.Blueprint) (*port.Blueprint, error) {
	pb := &port.ResponseBody{}
	resp, err := portClient.Client.R().
		SetResult(&pb).
		SetBody(blueprint).
		Post("v1/blueprints")
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to create blueprint, got: %s", resp.Body())
	}
	return &pb.Blueprint, nil
}

func PatchBlueprint(portClient *cli.PortClient, blueprint port.Blueprint) (*port.Blueprint, error) {
	pb := &port.ResponseBody{}
	resp, err := portClient.Client.R().
		SetResult(&pb).
		SetBody(blueprint).
		Patch(fmt.Sprintf("v1/blueprints/%s", blueprint.Identifier))
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to patch blueprint, got: %s", resp.Body())
	}
	return &pb.Blueprint, nil
}

func DeleteBlueprint(portClient *cli.PortClient, blueprintIdentifier string) error {
	pb := &port.ResponseBody{}
	resp, err := portClient.Client.R().
		SetResult(&pb).
		Delete(fmt.Sprintf("v1/blueprints/%s", blueprintIdentifier))
	if err != nil {
		return err
	}
	if !pb.OK {
		return fmt.Errorf("failed to delete blueprint, got: %s", resp.Body())
	}
	return nil
}
