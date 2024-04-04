package blueprint

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/klog/v2"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

func NewBlueprint(portClient *cli.PortClient, blueprint port.Blueprint, upsert bool) (*port.Blueprint, error) {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)

	if err != nil {
		if upsert {
			if strings.Contains(err.Error(), "already exists") {
				return PatchBlueprint(portClient, blueprint)
			}
		}
		return nil, fmt.Errorf("error creating blueprint: %v", err)
	}

	if err != nil {
		return nil, fmt.Errorf("error authenticating with Port: %v", err)
	}

	bp, err := cli.CreateBlueprint(portClient, blueprint)
	if err != nil {
		return nil, fmt.Errorf("error creating Port blueprint: %v", err)
	}
	return bp, nil
}

func NewBlueprintAction(portClient *cli.PortClient, blueprintIdentifier string, action port.Action, upsert bool) (*port.Action, error) {
	_, err := portClient.Authenticate(context.Background(), portClient.ClientID, portClient.ClientSecret)
	if err != nil {
		if upsert {
			if strings.Contains(err.Error(), "already exists") {
				return cli.PatchAction(portClient, blueprintIdentifier, action)
			}

			return nil, fmt.Errorf("error creating blueprint action: %v", err)
		}

		return nil, fmt.Errorf("error authenticating with Port: %v", err)
	}

	act, err := cli.CreateAction(portClient, blueprintIdentifier, action)
	if err != nil {
		return nil, fmt.Errorf("error creating Port blueprint action: %v", err)
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

const (
	KindXRD = "CompositeResourceDefinition"
	KindCRD = "CustomResourceDefinition"
)

func CreateSchemasFromCRD(portClient *cli.PortClient, crds []v1.CustomResourceDefinition) error {
	const (
		KindXRD = "CompositeResourceDefinition"
		KindCRD = "CustomResourceDefinition"
	)

	for _, crd := range crds {
		latestCRDVersion := crd.Spec.Versions[0]
		bs := &port.Schema{}
		as := &port.ActionUserInputs{}

		bytes, err := json.Marshal(latestCRDVersion.Schema.OpenAPIV3Schema)

		if err != nil {
			return fmt.Errorf("error marshaling schema: %v", err)
		}

		err = json.Unmarshal(bytes, &bs)
		if err != nil {
			return fmt.Errorf("error unmarshaling schema into blueprint schema: %v", err)
		}

		err = json.Unmarshal(bytes, &as)
		if err != nil {
			return fmt.Errorf("error unmarshaling schema into action schema: %v", err)
		}

		bp := port.Blueprint{
			Identifier: crd.Spec.Names.Singular,
			Title:      crd.Spec.Names.Singular,
			Schema:     *bs,
		}

		act := port.Action{
			Identifier: crd.Spec.Names.Singular,
			Title:      crd.Spec.Names.Singular,
			UserInputs: *as,
			Trigger:    "CREATE",
			InvocationMethod: &port.InvocationMethod{
				Type:                 "GITHUB",
				Organization:         "org_goes_here",
				Repository:           "repo_goes_here",
				Workflow:             "workflow_goes_here.yml",
				OmitPayload:          false,
				OmitUserInputs:       true,
				ReportWorkflowStatus: true,
			},
		}

		_, err = NewBlueprint(portClient, bp, true)
		if err != nil {
			klog.Errorf("error creating blueprint: %v", err)
		}

		_, err = NewBlueprintAction(portClient, bp.Identifier, act, true)
		if err != nil {
			klog.Errorf("error creating blueprint action: %v", err)
		}
	}

	return nil
}
