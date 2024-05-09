package cli

import (
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

func CreateAction(portClient *PortClient, blueprintIdentifier string, action port.Action) (*port.Action, error) {
	pb := &port.ResponseBody{}
	resp, err := portClient.Client.R().
		SetResult(&pb).
		SetBody(action).
		Post(fmt.Sprintf("v1/blueprints/%s/actions/", blueprintIdentifier))
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to create action, got: %s", resp.Body())
	}
	return &pb.Action, nil
}

func UpdateAction(portClient *PortClient, blueprintIdentifier string, action port.Action) (*port.Action, error) {
	pb := &port.ResponseBody{}
	resp, err := portClient.Client.R().
		SetResult(&pb).
		SetBody(action).
		Put(fmt.Sprintf("v1/blueprints/%s/actions/%s", blueprintIdentifier, action.Identifier))
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to patch action, got: %s", resp.Body())
	}
	return &pb.Action, nil
}

func GetAction(portClient *PortClient, blueprintIdentifier string, actionIdentifier string) (*port.Action, error) {
	pb := &port.ResponseBody{}
	resp, err := portClient.Client.R().
		SetResult(&pb).
		Get(fmt.Sprintf("v1/blueprints/%s/actions/%s", blueprintIdentifier, actionIdentifier))
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to get action, got: %s", resp.Body())
	}
	return &pb.Action, nil
}
