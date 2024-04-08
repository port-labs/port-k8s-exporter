package crd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/jq"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"golang.org/x/exp/slices"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	KindCRD = "CustomResourceDefinition"
)

var invisibleFields = []string{
	"writeConnectionSecretToRef",
	"publishConnectionDetailsTo",
	"resourceRefs",
	"environmentConfigRefs",
	"compositeDeletePolicy",
	"resourceRef",
	"claimRefs",
	"compositionUpdatePolicy",
	"compositionRevisionSelector",
	"compositionRevisionRef",
	"compositionSelector",
	"compositionRef",
	"claimRef",
}

func CreateKindConfigFromCRD(crd v1.CustomResourceDefinition) port.Resource {
	resource := crd.Spec.Names.Kind
	group := crd.Spec.Group
	version := crd.Spec.Versions[0].Name
	kindConfig := port.Resource{
		Kind: group + "/" + version + "/" + resource,
		Selector: port.Selector{
			Query: "true",
		},
		Port: port.Port{
			Entity: port.EntityMappings{
				Mappings: []port.EntityMapping{
					{
						Identifier: ".metadata.name",
						Blueprint:  "\"" + crd.Spec.Names.Singular + "\"", // Blueprint is a JQ query, so that way we hardcoded it
						Title:      ".metadata.name",
						Properties: map[string]string{
							"*": ".spec",
						},
					},
				},
			},
		},
	}
	return kindConfig
}

func AutodiscoverCRDsToActions(exporterConfig *port.Config, portConfig *port.IntegrationAppConfig, k8sClient *k8s.Client, portClient *cli.PortClient) []v1.CustomResourceDefinition {
	crdsMatchedPattern := make([]v1.CustomResourceDefinition, 0)

	if portConfig.CRDSToDiscover == "" {
		klog.Info("Discovering CRDs is disabled")
		return crdsMatchedPattern
	}

	klog.Infof("Discovering CRDs/XRDs with pattern: %s", portConfig.CRDSToDiscover)
	crds, err := k8sClient.ApiExtensionClient.CustomResourceDefinitions().List(context.Background(), metav1.ListOptions{})

	for _, crd := range crds.Items {
		mapCrd, err := goutils.StructToMap(crd)

		if err != nil {
			klog.Errorf("Error converting CRD to map: %s", err.Error())
			continue
		}

		match, err := jq.ParseBool(portConfig.CRDSToDiscover, mapCrd)

		if err != nil {
			klog.Errorf("Error running jq on crd CRD: %s", err.Error())
			continue
		}
		if match {
			crdsMatchedPattern = append(crdsMatchedPattern, crd)
		}
	}

	if err != nil {
		klog.Errorf("Error listing CRDs: %s", err.Error())
	}

	for _, crd := range crdsMatchedPattern {
		actions, bp, err := ConvertToPortSchema(crd)
		if err != nil {
			klog.Errorf("Error converting CRD to Port schemas: %s", err.Error())
			continue
		}

		_, err = blueprint.NewBlueprint(portClient, *bp, true)
		if err != nil {
			klog.Errorf("Error creating blueprint: %s", err.Error())
		}

		for _, act := range actions {
			_, err = blueprint.NewBlueprintAction(portClient, bp.Identifier, act)
			if err != nil {
				klog.Errorf("Error creating blueprint action: %s", err.Error())
			}
		}
	}

	for _, crd := range crdsMatchedPattern {
		portConfig.Resources = append(portConfig.Resources, CreateKindConfigFromCRD(crd))
	}

	return crdsMatchedPattern
}

func BuildCreateAction(crd v1.CustomResourceDefinition, as *port.ActionUserInputs, apiVersionProperty, kindProperty, nameProperty port.ActionProperty, invocation port.InvocationMethod, bp port.Blueprint) port.Action {
	createActionProperties := goutils.MergeMaps(
		as.Properties,
		map[string]port.ActionProperty{"apiVersion": apiVersionProperty, "kind": kindProperty, "name": nameProperty},
	)

	crtAct := port.Action{
		Identifier: "create_" + crd.Spec.Names.Singular,
		Title:      "Create " + crd.Spec.Names.Singular,
		UserInputs: port.ActionUserInputs{
			Properties: createActionProperties,
			Required:   append(as.Required, "name"),
		},
		Trigger:          "CREATE",
		InvocationMethod: &invocation,
	}

	return crtAct
}

func BuildUpdateAction(crd v1.CustomResourceDefinition, as *port.ActionUserInputs, apiVersionProperty, kindProperty port.ActionProperty, invocation port.InvocationMethod, bp port.Blueprint) port.Action {
	for k, v := range as.Properties {
		updatedStruct := v

		defaultMap := make(map[string]string)
		defaultMap["jqQuery"] = ".entity.properties." + k
		updatedStruct.Default = defaultMap

		as.Properties[k] = updatedStruct
	}

	updateProperties := goutils.MergeMaps(
		as.Properties,
		map[string]port.ActionProperty{"apiVersion": apiVersionProperty, "kind": kindProperty},
	)

	uptAct := port.Action{
		Identifier: "update_" + crd.Spec.Names.Singular,
		Title:      "Update " + crd.Spec.Names.Singular,
		UserInputs: port.ActionUserInputs{
			Properties: updateProperties,
			Required:   as.Required,
		},
		Trigger:          "DAY-2",
		InvocationMethod: &invocation,
	}

	return uptAct
}

func BuildDeleteAction(crd v1.CustomResourceDefinition, apiVersionProperty, kindProperty port.ActionProperty, invocation port.InvocationMethod) port.Action {
	dltAct := port.Action{
		Identifier: "delete_" + crd.Spec.Names.Singular,
		Title:      "Delete " + crd.Spec.Names.Singular,
		Trigger:    "DELETE",
		UserInputs: port.ActionUserInputs{
			Properties: map[string]port.ActionProperty{
				"apiVersion": apiVersionProperty,
				"kind":       kindProperty,
			},
		},
		InvocationMethod: &invocation,
	}

	return dltAct
}

func ConvertToPortSchema(crd v1.CustomResourceDefinition) ([]port.Action, *port.Blueprint, error) {
	latestCRDVersion := crd.Spec.Versions[0]
	bs := &port.Schema{}
	as := &port.ActionUserInputs{}
	notVisible := new(bool) // Using a pointer to bool to avoid the omitempty of false values
	*notVisible = false

	var spec v1.JSONSchemaProps

	// If the CRD has a spec field, use that as the schema - as it's a best practice but not required by k8s
	if _, ok := latestCRDVersion.Schema.OpenAPIV3Schema.Properties["spec"]; ok {
		spec = latestCRDVersion.Schema.OpenAPIV3Schema.Properties["spec"]
	} else {
		spec = *latestCRDVersion.Schema.OpenAPIV3Schema
	}

	// Convert integer types to number as Port does not yet support integers
	for i, v := range spec.Properties {
		if v.Type == "integer" {
			v.Type = "number"
			v.Format = ""
			spec.Properties[i] = v
		}
	}

	bytes, err := json.Marshal(&spec)

	if err != nil {
		return nil, nil, fmt.Errorf("error marshaling schema: %v", err)
	}

	err = json.Unmarshal(bytes, &bs)
	if err != nil {
		return nil, nil, fmt.Errorf("error unmarshaling schema into blueprint schema: %v", err)
	}

	err = json.Unmarshal(bytes, &as)
	if err != nil {
		return nil, nil, fmt.Errorf("error unmarshaling schema into action schema: %v", err)
	}

	bp := port.Blueprint{
		Identifier: crd.Spec.Names.Singular,
		Title:      crd.Spec.Names.Singular,
		Schema:     *bs,
	}

	// Hide fields that are not commonly needed by default, with this approach we can still let the platform engineer show them afterwards if needed
	for k, v := range as.Properties {
		if slices.Contains(invisibleFields, k) {
			v.Visible = notVisible
			as.Properties[k] = v
		}
	}

	apiVersionProperty := port.ActionProperty{
		Type:    "string",
		Visible: notVisible,
		Default: crd.Spec.Group + "/" + crd.Spec.Versions[0].Name,
	}

	kindProperty := port.ActionProperty{
		Type:    "string",
		Visible: notVisible,
		Default: crd.Spec.Names.Kind,
	}

	nameProperty := port.ActionProperty{
		Type:  "string",
		Title: crd.Spec.Names.Singular + " Name",
	}

	invocation := port.InvocationMethod{
		Type:                 "GITHUB",
		Organization:         "danielsinai",
		Repository:           "control-plane-demo",
		Workflow:             "sync-control-plane-direct.yml",
		OmitPayload:          false,
		OmitUserInputs:       true,
		ReportWorkflowStatus: true,
	}

	actions := []port.Action{
		BuildCreateAction(crd, as, apiVersionProperty, kindProperty, nameProperty, invocation, bp),
		BuildUpdateAction(crd, as, apiVersionProperty, kindProperty, invocation, bp),
		BuildDeleteAction(crd, apiVersionProperty, kindProperty, invocation),
	}

	return actions, &bp, nil
}
