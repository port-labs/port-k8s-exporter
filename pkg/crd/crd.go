package crd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/jq"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"golang.org/x/exp/slices"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	KindCRD        = "CustomResourceDefinition"
	K8SIcon        = "Cluster"
	CrossplaneIcon = "Crossplane"
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
	return fmt.Sprintf("This action was automatically generated from a Custom Resource Definition (CRD) in the cluster. It allows you to create, update, and delete %s resources. To complete the setup, go to [Autodiscovery Guide](https://docs.getport.io)", crd.Spec.Names.Singular)
}
func getIconFromCRD(crd v1.CustomResourceDefinition) string {
	if len(crd.ObjectMeta.OwnerReferences) > 0 && crd.ObjectMeta.OwnerReferences[0].Kind == "CompositeResourceDefinition" {
		return CrossplaneIcon
	}
	return K8SIcon
}

func buildCreateAction(crd v1.CustomResourceDefinition, as *port.ActionUserInputs, apiVersionProperty port.ActionProperty, kindProperty port.ActionProperty, nameProperty port.ActionProperty, namespaceProperty port.ActionProperty, invocation port.InvocationMethod) port.Action {
	createActionProperties := goutils.MergeMaps(
		as.Properties,
		map[string]port.ActionProperty{"apiVersion": apiVersionProperty, "kind": kindProperty, "name": nameProperty},
	)

	crtAct := port.Action{
		Identifier: "create_" + crd.Spec.Names.Singular,
		Title:      "Create " + strings.Title(crd.Spec.Names.Singular),
		Icon:       getIconFromCRD(crd),
		UserInputs: port.ActionUserInputs{
			Properties: createActionProperties,
			Required:   append(as.Required, "name"),
		},
		Description:      getDescriptionFromCRD(crd),
		Trigger:          "CREATE",
		InvocationMethod: &invocation,
	}

	if isCRDNamespacedScoped(crd) {
		crtAct.UserInputs.Properties["namespace"] = namespaceProperty
		crtAct.UserInputs.Required = append(crtAct.UserInputs.Required, "namespace")
	}

	return crtAct
}

func buildUpdateAction(crd v1.CustomResourceDefinition, as *port.ActionUserInputs, apiVersionProperty port.ActionProperty, kindProperty port.ActionProperty, namespaceProperty port.ActionProperty, invocation port.InvocationMethod) port.Action {
	if isCRDNamespacedScoped(crd) {
		as.Properties["namespace"] = namespaceProperty
		as.Required = append(as.Required, "namespace")
	}

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

	updtAct := port.Action{
		Identifier:  "update_" + crd.Spec.Names.Singular,
		Title:       "Update " + strings.Title(crd.Spec.Names.Singular),
		Icon:        getIconFromCRD(crd),
		Description: getDescriptionFromCRD(crd),
		UserInputs: port.ActionUserInputs{
			Properties: updateProperties,
			Required:   as.Required,
		},
		Trigger:          "DAY-2",
		InvocationMethod: &invocation,
	}

	return updtAct
}

func buildDeleteAction(crd v1.CustomResourceDefinition, apiVersionProperty port.ActionProperty, kindProperty port.ActionProperty, namespaceProperty port.ActionProperty, invocation port.InvocationMethod) port.Action {
	dltAct := port.Action{
		Identifier:  "delete_" + crd.Spec.Names.Singular,
		Title:       "Delete " + strings.Title(crd.Spec.Names.Singular),
		Icon:        getIconFromCRD(crd),
		Description: getDescriptionFromCRD(crd),
		Trigger:     "DELETE",
		UserInputs: port.ActionUserInputs{
			Properties: map[string]port.ActionProperty{
				"apiVersion": apiVersionProperty,
				"kind":       kindProperty,
			},
		},
		InvocationMethod: &invocation,
	}

	if isCRDNamespacedScoped(crd) {
		visible := new(bool) // Using a pointer to bool to avoid the omitempty of false values
		*visible = false
		namespaceProperty.Visible = visible
		dltAct.UserInputs.Properties["namespace"] = namespaceProperty
		dltAct.UserInputs.Required = append(dltAct.UserInputs.Required, "namespace")
	}

	return dltAct
}

func convertToPortSchema(crd v1.CustomResourceDefinition) ([]port.Action, *port.Blueprint, error) {
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
		OmitPayload:          false,
		OmitUserInputs:       true,
		ReportWorkflowStatus: true,
	}

	actions := []port.Action{
		buildCreateAction(crd, as, apiVersionProperty, kindProperty, nameProperty, namespaceProperty, invocation),
		buildUpdateAction(crd, as, apiVersionProperty, kindProperty, namespaceProperty, invocation),
		buildDeleteAction(crd, apiVersionProperty, kindProperty, namespaceProperty, invocation),
	}

	return actions, &bp, nil
}

func findMatchingCRDs(crds []v1.CustomResourceDefinition, pattern string) []v1.CustomResourceDefinition {
	matchedCRDs := make([]v1.CustomResourceDefinition, 0)

	for _, crd := range crds {
		mapCrd, err := goutils.StructToMap(crd)

		if err != nil {
			klog.Errorf("Error converting CRD to map: %s", err.Error())
			continue
		}

		match, err := jq.ParseBool(pattern, mapCrd)

		if err != nil {
			klog.Errorf("Error running jq on crd CRD: %s", err.Error())
			continue
		}
		if match {
			matchedCRDs = append(matchedCRDs, crd)
		}
	}

	return matchedCRDs
}

func handleMatchingCRD(crds []v1.CustomResourceDefinition, pattern string, portConfig *port.IntegrationAppConfig, portClient *cli.PortClient) {
	matchedCRDs := findMatchingCRDs(crds, pattern)

	for _, crd := range matchedCRDs {
		actions, bp, err := convertToPortSchema(crd)
		if err != nil {
			klog.Errorf("Error converting CRD to Port schemas: %s", err.Error())
			continue
		}

		_, err = blueprint.NewBlueprint(portClient, *bp)

		if err != nil && strings.Contains(err.Error(), "taken") {
			klog.Infof("Blueprint already exists, patching blueprint properties")
			_, err = blueprint.PatchBlueprint(portClient, port.Blueprint{Schema: bp.Schema, Identifier: bp.Identifier})
			if err != nil {
				klog.Errorf("Error patching blueprint: %s", err.Error())
			}
		}

		if err != nil {
			klog.Errorf("Error creating blueprint: %s", err.Error())
		}

		for _, act := range actions {
			_, err = blueprint.NewBlueprintAction(portClient, bp.Identifier, act)
			if err != nil {
				if strings.Contains(err.Error(), "taken") {
					if portConfig.OverwriteCRDsActions == true {
						_, err = blueprint.UpdateBlueprintAction(portClient, bp.Identifier, act)
						if err != nil {
							klog.Errorf("Error updating blueprint action: %s", err.Error())
						}
					} else {
						klog.Infof("Action already exists, if you wish to overwrite it, delete it first or provide the configuration overwriteCrdsActions: true, in the exporter configuration and resync")
					}
				} else {
					klog.Errorf("Error creating blueprint action: %s", err.Error())
				}
			}
		}
	}
}

func AutodiscoverCRDsToActions(portConfig *port.IntegrationAppConfig, apiExtensionsClient apiextensions.ApiextensionsV1Interface, portClient *cli.PortClient) {
	if portConfig.CRDSToDiscover == "" {
		klog.Info("Discovering CRDs is disabled")
		return
	}

	klog.Infof("Discovering CRDs/XRDs with pattern: %s", portConfig.CRDSToDiscover)
	crds, err := apiExtensionsClient.CustomResourceDefinitions().List(context.Background(), metav1.ListOptions{})

	if err != nil {
		klog.Errorf("Error listing CRDs: %s", err.Error())
		return
	}

	handleMatchingCRD(crds.Items, portConfig.CRDSToDiscover, portConfig, portClient)
}
