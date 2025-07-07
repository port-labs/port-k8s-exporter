package crd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/jq"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"golang.org/x/exp/slices"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	KindCRD               = "CustomResourceDefinition"
	K8SIcon               = "Cluster"
	CrossplaneIcon        = "Crossplane"
	NestedSchemaSeparator = "__"
)

func createKindConfigFromCRD(crd v1.CustomResourceDefinition) port.Resource {
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
							"namespace": ".metadata.namespace",
							"*":         ".spec",
						},
					},
				},
			},
		},
	}
	return kindConfig
}

func isCRDNamespacedScoped(crd v1.CustomResourceDefinition) bool {
	return crd.Spec.Scope == v1.NamespaceScoped
}

func getDescriptionFromCRD(crd v1.CustomResourceDefinition) string {
	return fmt.Sprintf("This action automatically generated from a Custom Resource Definition (CRD) in the cluster. Allows you to create, update, and delete %s resources. To complete the setup of this action, follow [this guide](https://docs.getport.io/guides-and-tutorials/manage-resources-using-k8s-crds)", crd.Spec.Names.Singular)
}
func getIconFromCRD(crd v1.CustomResourceDefinition) string {
	if len(crd.ObjectMeta.OwnerReferences) > 0 && crd.ObjectMeta.OwnerReferences[0].Kind == "CompositeResourceDefinition" {
		return CrossplaneIcon
	}
	return K8SIcon
}

func buildCreateAction(crd v1.CustomResourceDefinition, as *port.ActionUserInputs, nameProperty port.ActionProperty, namespaceProperty port.ActionProperty, invocation port.InvocationMethod) port.Action {
	createActionProperties := goutils.MergeMaps(
		as.Properties,
		map[string]port.ActionProperty{"name": nameProperty},
	)

	crtAct := port.Action{
		Identifier: "create_" + crd.Spec.Names.Singular,
		Title:      "Create " + strings.Title(crd.Spec.Names.Singular),
		Icon:       getIconFromCRD(crd),
		Trigger: &port.Trigger{
			Type:                "self-service",
			Operation:           "CREATE",
			BlueprintIdentifier: crd.Spec.Names.Singular,
			UserInputs: &port.ActionUserInputs{
				Properties: createActionProperties,
				Required:   append(as.Required, "name"),
			},
		},
		Description:      getDescriptionFromCRD(crd),
		InvocationMethod: &invocation,
	}

	if isCRDNamespacedScoped(crd) {
		crtAct.Trigger.UserInputs.Properties["namespace"] = namespaceProperty
		crtAct.Trigger.UserInputs.Required = append(crtAct.Trigger.UserInputs.Required, "namespace")
	}

	return crtAct
}

func buildUpdateAction(crd v1.CustomResourceDefinition, as *port.ActionUserInputs, invocation port.InvocationMethod) port.Action {
	for k, v := range as.Properties {
		updatedStruct := v

		defaultMap := make(map[string]string)
		// Blueprint schema differs from the action schema, as it not shallow - this JQ pattern assign the defaults from the entity nested schema to the action shallow one
		defaultMap["jqQuery"] = ".entity.properties." + strings.Replace(k, NestedSchemaSeparator, ".", -1)
		updatedStruct.Default = defaultMap

		as.Properties[k] = updatedStruct
	}

	updtAct := port.Action{
		Identifier:  "update_" + crd.Spec.Names.Singular,
		Title:       "Update " + strings.Title(crd.Spec.Names.Singular),
		Icon:        getIconFromCRD(crd),
		Description: getDescriptionFromCRD(crd),
		Trigger: &port.Trigger{
			Type:                "self-service",
			Operation:           "DAY-2",
			BlueprintIdentifier: crd.Spec.Names.Singular,
			UserInputs: &port.ActionUserInputs{
				Properties: as.Properties,
				Required:   as.Required,
			},
		},
		InvocationMethod: &invocation,
	}

	return updtAct
}

func buildDeleteAction(crd v1.CustomResourceDefinition, invocation port.InvocationMethod) port.Action {
	dltAct := port.Action{
		Identifier:  "delete_" + crd.Spec.Names.Singular,
		Title:       "Delete " + strings.Title(crd.Spec.Names.Singular),
		Icon:        getIconFromCRD(crd),
		Description: getDescriptionFromCRD(crd),
		Trigger: &port.Trigger{
			Type:                "self-service",
			BlueprintIdentifier: crd.Spec.Names.Singular,
			UserInputs: &port.ActionUserInputs{
				Properties: map[string]port.ActionProperty{},
			},
			Operation: "DELETE",
		},

		InvocationMethod: &invocation,
	}

	return dltAct
}

func adjustSchemaToPortSchemaCompatibilityLevel(spec *v1.JSONSchemaProps) {
	for i, v := range spec.Properties {
		switch v.Type {
		case "object":
			adjustSchemaToPortSchemaCompatibilityLevel(&v)
			spec.Properties[i] = v
		case "integer":
			v.Type = "number"
			v.Format = ""
			spec.Properties[i] = v
		case "":
			if v.AnyOf != nil && len(v.AnyOf) > 0 {
				possibleTypes := make([]string, 0)
				for _, anyOf := range v.AnyOf {
					possibleTypes = append(possibleTypes, anyOf.Type)
				}

				// Prefer string over other types
				if slices.Contains(possibleTypes, "string") {
					v.Type = "string"
				} else {
					v.Type = possibleTypes[0]
				}
			}
			spec.Properties[i] = v
		}
	}
}

func convertToPortSchemas(crd v1.CustomResourceDefinition) ([]port.Action, *port.Blueprint, error) {
	latestCRDVersion := crd.Spec.Versions[0]
	bs := &port.BlueprintSchema{}
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

	// Adjust schema to be compatible with Port schema
	// Port's schema complexity is not rich as k8s, so we need to adjust some types and formats so we can bridge this gap
	adjustSchemaToPortSchemaCompatibilityLevel(&spec)

	bytes, err := json.Marshal(&spec)
	if err != nil {
		return nil, nil, fmt.Errorf("error marshaling schema: %v", err)
	}

	err = json.Unmarshal(bytes, &bs)
	if err != nil {
		return nil, nil, fmt.Errorf("error unmarshaling schema into blueprint schema: %v", err)
	}

	// Make nested schemas shallow with `NestedSchemaSeparator`(__) separator
	shallowedSchema := ShallowJsonSchema(&spec, NestedSchemaSeparator)
	bytesNested, err := json.Marshal(&shallowedSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("error marshaling schema: %v", err)
	}

	err = json.Unmarshal(bytesNested, &as)

	if err != nil {
		return nil, nil, fmt.Errorf("error unmarshaling schema into action schema: %v", err)
	}

	for k, v := range as.Properties {
		if !slices.Contains(as.Required, k) {
			v.Visible = new(bool)
			// Not required fields should not be visible, and also shouldn't be applying default values in Port's side, instead we should let k8s apply the defaults
			*v.Visible = false
			v.Default = nil
			as.Properties[k] = v
		}

		as.Properties[k] = v
	}

	if isCRDNamespacedScoped(crd) {
		bs.Properties["namespace"] = port.Property{
			Type:  "string",
			Title: "Namespace",
		}
	}

	bp := port.Blueprint{
		Identifier: crd.Spec.Names.Singular,
		Title:      strings.Title(crd.Spec.Names.Singular),
		Icon:       getIconFromCRD(crd),
		Schema:     *bs,
	}

	nameProperty := port.ActionProperty{
		Type:    "string",
		Title:   crd.Spec.Names.Singular + " Name",
		Pattern: "^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$",
	}

	namespaceProperty := port.ActionProperty{
		Type:    "string",
		Title:   "Namespace",
		Default: "default",
	}

	invocation := port.InvocationMethod{
		Type:                 "GITHUB",
		Organization:         "<fill-organization-name>",
		Repository:           "<fill-repository-name>",
		Workflow:             "sync-control-plane-direct.yml",
		ReportWorkflowStatus: true,
		WorkflowInputs: map[string]interface{}{
			"operation":      "{{.trigger.operation}}",
			"triggeringUser": "{{ .trigger.by.user.email }}",
			"runId":          "{{ .run.id }}",
			"manifest": map[string]interface{}{
				"apiVersion": crd.Spec.Group + "/" + crd.Spec.Versions[0].Name,
				"kind":       crd.Spec.Names.Kind,
				"metadata": map[string]interface{}{
					"{{if (.entity | has(\"identifier\")) then \"name\" else null end}}":                "{{.entity.\"identifier\"}}",
					"{{if (.inputs | has(\"name\")) then \"name\" else null end}}":                      "{{.inputs.\"name\"}}",
					"{{if (.entity.properties | has(\"namespace\")) then \"namespace\" else null end}}": "{{.entity.properties.\"namespace\"}}",
					"{{if (.inputs | has(\"namespace\")) then \"namespace\" else null end}}":            "{{.inputs.\"namespace\"}}",
				},
				"spec": "{{ .inputs | to_entries | map(if .key | contains(\"__\") then .key |= split(\"__\") else . end) | reduce .[] as $item ({}; if $item.key | type  == \"array\" then setpath($item.key;$item.value) else setpath([$item.key];$item.value) end) | del(.name) | del (.namespace) }}",
			},
		},
	}

	actions := []port.Action{
		buildCreateAction(crd, as, nameProperty, namespaceProperty, invocation),
		buildUpdateAction(crd, as, invocation),
		buildDeleteAction(crd, invocation),
	}

	return actions, &bp, nil
}

func findMatchingCRDs(crds []v1.CustomResourceDefinition, pattern string) []v1.CustomResourceDefinition {
	matchedCRDs := make([]v1.CustomResourceDefinition, 0)

	for _, crd := range crds {
		mapCrd, err := goutils.StructToMap(crd)

		if err != nil {
			logger.Errorf("Error converting CRD to map: %s", err.Error())
			continue
		}

		match, err := jq.ParseBool(pattern, mapCrd)

		if err != nil {
			logger.Errorf("Error running jq on crd CRD: %s", err.Error())
			continue
		}
		if match {
			matchedCRDs = append(matchedCRDs, crd)
		}
	}

	return matchedCRDs
}

func handleCRD(crds []v1.CustomResourceDefinition, portConfig *port.IntegrationAppConfig, portClient *cli.PortClient) {
	matchedCRDs := findMatchingCRDs(crds, portConfig.CRDSToDiscover)

	for _, crd := range matchedCRDs {
		portConfig.Resources = append(portConfig.Resources, createKindConfigFromCRD(crd))
		actions, bp, err := convertToPortSchemas(crd)
		if err != nil {
			logger.Errorf("Error converting CRD to Port schemas: %s", err.Error())
			continue
		}

		_, err = blueprint.NewBlueprint(portClient, *bp)

		if err != nil && strings.Contains(err.Error(), "taken") {
			logger.Infof("Blueprint already exists, patching blueprint properties")
			_, err = blueprint.PatchBlueprint(portClient, port.Blueprint{Schema: bp.Schema, Identifier: bp.Identifier})
			if err != nil {
				logger.Errorf("Error patching blueprint: %s", err.Error())
			}
		}

		if err != nil {
			logger.Errorf("Error creating blueprint: %s", err.Error())
		}

		for _, act := range actions {
			_, err = cli.CreateAction(portClient, act)
			if err != nil {
				if strings.Contains(err.Error(), "taken") {
					if portConfig.OverwriteCRDsActions {
						_, err = cli.UpdateAction(portClient, act)
						if err != nil {
							logger.Errorf("Error updating blueprint action: %s", err.Error())
						}
					} else {
						logger.Infof("Action already exists, if you wish to overwrite it, delete it first or provide the configuration overwriteCrdsActions: true, in the exporter configuration and resync")
					}
				} else {
					logger.Errorf("Error creating blueprint action: %s", err.Error())
				}
			}
		}
	}
}

func AutodiscoverCRDsToActions(portConfig *port.IntegrationAppConfig, apiExtensionsClient apiextensions.ApiextensionsV1Interface, portClient *cli.PortClient) {
	if portConfig.CRDSToDiscover == "" {
		logger.Info("Discovering CRDs is disabled")
		return
	}

	logger.Infof("Discovering CRDs/XRDs with pattern: %s", portConfig.CRDSToDiscover)
	crds, err := apiExtensionsClient.CustomResourceDefinitions().List(context.Background(), metav1.ListOptions{})

	if err != nil {
		logger.Errorf("Error listing CRDs: %s", err.Error())
		return
	}

	handleCRD(crds.Items, portConfig, portClient)
}
