package cli

import (
	"fmt"
	"slices"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

func CreateBlueprint(portClient *PortClient, blueprint port.Blueprint) (*port.Blueprint, error) {
	return InnerCreateBlueprint(portClient, blueprint, true)
}

func CreateBlueprintWithoutPage(portClient *PortClient, blueprint port.Blueprint) (*port.Blueprint, error) {
	return InnerCreateBlueprint(portClient, blueprint, false)
}

func InnerCreateBlueprint(portClient *PortClient, blueprint port.Blueprint, shouldCreatePage bool) (*port.Blueprint, error) {
	pb := &port.ResponseBody{}
	resp, err := portClient.Client.R().
		SetResult(&pb).
		SetBody(blueprint).
		SetQueryParam("create_catalog_page", fmt.Sprintf("%t", shouldCreatePage)).
		Post("v1/blueprints")
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to create blueprint, got: %s", resp.Body())
	}
	return &pb.Blueprint, nil
}

func PatchBlueprint(portClient *PortClient, blueprint port.Blueprint) (*port.Blueprint, error) {
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

func DeleteBlueprint(portClient *PortClient, blueprintIdentifier string) error {
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

func DeleteBlueprintEntities(portClient *PortClient, blueprintIdentifier string) error {
	pb := &port.ResponseBody{}
	resp, err := portClient.Client.R().
		SetResult(&pb).
		Delete(fmt.Sprintf("v1/blueprints/%s/all-entities?delete_blueprint=false", blueprintIdentifier))
	if err != nil {
		return err
	}

	if !pb.OK {
		return fmt.Errorf("failed to delete blueprint, got: %s", resp.Body())
	}

	migrationId := pb.MigrationId

	inProgressStatuses := []string{
		"RUNNING",
		"INITIALIZING",
		"PENDING",
	}

	isCompleted := false
	for !isCompleted {
		migrResp, migrErr := portClient.Client.R().
			SetResult(&pb).
			Get(fmt.Sprintf("v1/migrations/%s", migrationId))

		if migrErr != nil {
			return fmt.Errorf("failed to fetch entities delete migration for '%s', got: %s", migrationId, migrResp.Body())
		}

		if slices.Contains(inProgressStatuses, pb.Migration.Status) {
			time.Sleep(2 * time.Second)
		} else {
			isCompleted = true
		}

	}

	return nil
}

func GetBlueprint(portClient *PortClient, blueprintIdentifier string) (*port.Blueprint, error) {
	pb := &port.ResponseBody{}
	resp, err := portClient.Client.R().
		SetResult(&pb).
		Get(fmt.Sprintf("v1/blueprints/%s", blueprintIdentifier))
	if err != nil {
		return nil, err
	}
	if !pb.OK {
		return nil, fmt.Errorf("failed to get blueprint, got: %s", resp.Body())
	}
	return &pb.Blueprint, nil
}
