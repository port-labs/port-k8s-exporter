package crd

import (
	"fmt"
	"slices"
	"testing"

	guuid "github.com/google/uuid"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	testUtils "github.com/port-labs/port-k8s-exporter/test_utils"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	fakeapiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1/fake"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
)

type Fixture struct {
	t                  *testing.T
	apiextensionClient *fakeapiextensionsv1.FakeApiextensionsV1
	portClient         *cli.PortClient
	portConfig         *port.IntegrationAppConfig
	stateKey           string
}

var (
	blueprintPrefix = "k8s-crd-test"
)

func getBlueprintId(stateKey string) string {
	return testUtils.GetBlueprintIdFromPrefixAndStateKey(blueprintPrefix, stateKey)
}

func deleteDefaultResources(stateKey string, portClient *cli.PortClient) {
	blueprintId := getBlueprintId(stateKey)
	_ = blueprint.DeleteBlueprintEntities(portClient, blueprintId)
	_ = blueprint.DeleteBlueprint(portClient, blueprintId)
}

func newFixture(t *testing.T, userAgent string, namespaced bool, crdsDiscoveryPattern string) *Fixture {

	stateKey := guuid.NewString()
	blueprintId := getBlueprintId(stateKey)
	apiExtensionsFakeClient := fakeapiextensionsv1.FakeApiextensionsV1{Fake: &clienttesting.Fake{}}

	apiExtensionsFakeClient.AddReactor("list", "customresourcedefinitions", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
		fakeCrd := &v1.CustomResourceDefinitionList{
			Items: []v1.CustomResourceDefinition{
				{
					Spec: v1.CustomResourceDefinitionSpec{
						Group: "testgroup",
						Names: v1.CustomResourceDefinitionNames{
							Kind:     "TestKind",
							Singular: blueprintId,
							Plural:   "testkinds",
						},
						Versions: []v1.CustomResourceDefinitionVersion{
							{
								Name: "v1",
								Schema: &v1.CustomResourceValidation{
									OpenAPIV3Schema: &v1.JSONSchemaProps{
										Type: "object",
										Properties: map[string]v1.JSONSchemaProps{
											"spec": {
												Type: "object",
												Properties: map[string]v1.JSONSchemaProps{
													"stringProperty": {
														Type: "string",
													},
													"intProperty": {
														Type: "integer",
													},
													"boolProperty": {
														Type: "boolean",
													},
													"nestedProperty": {
														Type: "object",
														Properties: map[string]v1.JSONSchemaProps{
															"nestedStringProperty": {
																Type: "string",
															},
														},
														Required: []string{"nestedStringProperty"},
													},
													"anyOfProperty": {
														AnyOf: []v1.JSONSchemaProps{
															{
																Type: "string",
															},
															{
																Type: "integer",
															},
														},
													},
												},
												Required: []string{"stringProperty", "nestedProperty"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		if namespaced {
			fakeCrd.Items[0].Spec.Scope = v1.NamespaceScoped
		} else {
			fakeCrd.Items[0].Spec.Scope = v1.ClusterScoped
		}

		return true, fakeCrd, nil
	})

	newConfig := &config.ApplicationConfiguration{
		ConfigFilePath:                  config.ApplicationConfig.ConfigFilePath,
		ResyncInterval:                  config.ApplicationConfig.ResyncInterval,
		PortBaseURL:                     config.ApplicationConfig.PortBaseURL,
		EventListenerType:               config.ApplicationConfig.EventListenerType,
		CreateDefaultResources:          config.ApplicationConfig.CreateDefaultResources,
		OverwriteConfigurationOnRestart: config.ApplicationConfig.OverwriteConfigurationOnRestart,
		Resources:                       config.ApplicationConfig.Resources,
		DeleteDependents:                config.ApplicationConfig.DeleteDependents,
		CreateMissingRelatedEntities:    config.ApplicationConfig.CreateMissingRelatedEntities,
		UpdateEntityOnlyOnDiff:          config.ApplicationConfig.UpdateEntityOnlyOnDiff,
		PortClientId:                    config.ApplicationConfig.PortClientId,
		PortClientSecret:                config.ApplicationConfig.PortClientSecret,
		StateKey:                        stateKey,
	}

	if userAgent == "" {
		userAgent = fmt.Sprintf("%s/0.1", stateKey)
	}

	portClient := cli.New(newConfig)
	deleteDefaultResources(stateKey, portClient)

	return &Fixture{
		t:                  t,
		portClient:         portClient,
		apiextensionClient: &apiExtensionsFakeClient,
		portConfig: &port.IntegrationAppConfig{
			CRDSToDiscover: crdsDiscoveryPattern,
		},
		stateKey: stateKey,
	}
}

func checkBlueprintAndActionsProperties(t *testing.T, f *Fixture, namespaced bool) {

	blueprintId := getBlueprintId(f.stateKey)
	bp, err := blueprint.GetBlueprint(f.portClient, blueprintId)
	if err != nil {
		t.Errorf("Error getting blueprint: %s", err.Error())
	}
	t.Run("Check blueprint", func(t *testing.T) {
		if bp == nil {
			t.Errorf("Blueprint not found")
		}
		if bp.Schema.Properties["stringProperty"].Type != "string" {
			t.Errorf("stringProperty type is not string")
		}
		if bp.Schema.Properties["intProperty"].Type != "number" {
			t.Errorf("intProperty type is not number")
		}
		if bp.Schema.Properties["boolProperty"].Type != "boolean" {
			t.Errorf("boolProperty type is not boolean")
		}
		if bp.Schema.Properties["anyOfProperty"].Type != "string" {
			t.Errorf("anyOfProperty type is not string")
		}
		if bp.Schema.Properties["nestedProperty"].Type != "object" {
			t.Errorf("nestedProperty type is not object")
		}
		if namespaced {
			if bp.Schema.Properties["namespace"].Type != "string" {
				t.Errorf("namespace type is not string")
			}
		} else {
			if _, ok := bp.Schema.Properties["namespace"]; ok {
				t.Errorf("namespace should not be present")
			}
		}
	})

	createAction, err := cli.GetAction(f.portClient, fmt.Sprintf("create_%s", blueprintId))
	if err != nil {
		t.Errorf("Error getting create action: %s", err.Error())
	}
	t.Run("Check create action", func(t *testing.T) {
		if createAction == nil {
			t.Errorf("Create action not found")
		}
		if createAction.Trigger.UserInputs.Properties["stringProperty"].Type != "string" {
			t.Errorf("stringProperty type is not string")
		}
		if createAction.Trigger.UserInputs.Properties["intProperty"].Type != "number" {
			t.Errorf("intProperty type is not number")
		}
		if createAction.Trigger.UserInputs.Properties["boolProperty"].Type != "boolean" {
			t.Errorf("boolProperty type is not boolean")
		}
		if createAction.Trigger.UserInputs.Properties["anyOfProperty"].Type != "string" {
			t.Errorf("anyOfProperty type is not string")
		}
		if _, ok := createAction.Trigger.UserInputs.Properties["nestedProperty"]; ok {
			t.Errorf("nestedProperty should not be present")
		}
		if createAction.Trigger.UserInputs.Properties["nestedProperty__nestedStringProperty"].Type != "string" {
			t.Errorf("nestedProperty__nestedStringProperty type is not string")
		}
		if namespaced {
			if createAction.Trigger.UserInputs.Properties["namespace"].Type != "string" {
				t.Errorf("namespace type is not string")
			}
		} else {
			if _, ok := createAction.Trigger.UserInputs.Properties["namespace"]; ok {
				t.Errorf("namespace should not be present")
			}
		}
		if slices.Contains(createAction.Trigger.UserInputs.Required, "stringProperty") == false {
			t.Errorf("stringProperty should be required")
		}
		if slices.Contains(createAction.Trigger.UserInputs.Required, "nestedProperty__nestedStringProperty") == false {
			t.Errorf("nestedProperty__nestedStringProperty should be required")
		}
		if slices.Contains(createAction.Trigger.UserInputs.Required, "nestedProperty") == true {
			t.Errorf("nestedProperty should not be required")
		}
	})

	updateAction, err := cli.GetAction(f.portClient, fmt.Sprintf("update_%s", blueprintId))
	if err != nil {
		t.Errorf("Error getting update action: %s", err.Error())
	}
	t.Run("Check update action", func(t *testing.T) {
		if updateAction == nil {
			t.Errorf("Update action not found")
		}
		if updateAction.Trigger.UserInputs.Properties["stringProperty"].Type != "string" {
			t.Errorf("stringProperty type is not string")
		}
		if updateAction.Trigger.UserInputs.Properties["intProperty"].Type != "number" {
			t.Errorf("intProperty type is not number")
		}
		if updateAction.Trigger.UserInputs.Properties["boolProperty"].Type != "boolean" {
			t.Errorf("boolProperty type is not boolean")
		}
		if updateAction.Trigger.UserInputs.Properties["anyOfProperty"].Type != "string" {
			t.Errorf("anyOfProperty type is not string")
		}
		if _, ok := createAction.Trigger.UserInputs.Properties["nestedProperty"]; ok {
			t.Errorf("nestedProperty should not be present")
		}
		if createAction.Trigger.UserInputs.Properties["nestedProperty__nestedStringProperty"].Type != "string" {
			t.Errorf("nestedProperty__nestedStringProperty type is not string")
		}
		if _, ok := updateAction.Trigger.UserInputs.Properties["namespace"]; ok {
			t.Errorf("namespace should not be present")
		}
		if slices.Contains(createAction.Trigger.UserInputs.Required, "stringProperty") == false {
			t.Errorf("stringProperty should be required")
		}
		if slices.Contains(createAction.Trigger.UserInputs.Required, "nestedProperty__nestedStringProperty") == false {
			t.Errorf("nestedProperty__nestedStringProperty should be required")
		}
		if slices.Contains(createAction.Trigger.UserInputs.Required, "nestedProperty") == true {
			t.Errorf("nestedProperty should not be required")
		}
	})

	deleteAction, err := cli.GetAction(f.portClient, fmt.Sprintf("delete_%s", blueprintId))
	if err != nil {
		t.Errorf("Error getting delete action: %s", err.Error())
	}
	t.Run("Check delete action", func(t *testing.T) {
		if deleteAction == nil {
			t.Errorf("Delete action not found")
		}
		// Delete action takes the namespace using control the payload feature
		if namespaced {
			if _, ok := deleteAction.Trigger.UserInputs.Properties["namespace"]; ok {
				t.Errorf("namespace should not be present")
			}
		} else {
			if _, ok := deleteAction.Trigger.UserInputs.Properties["namespace"]; ok {
				t.Errorf("namespace should not be present")
			}
		}
	})
}

func TestCRD_crd_autoDiscoverCRDsToActionsClusterScoped(t *testing.T) {
	f := newFixture(t, "", false, "true")

	blueprintId := getBlueprintId(f.stateKey)

	AutodiscoverCRDsToActions(f.portConfig, f.apiextensionClient, f.portClient)

	checkBlueprintAndActionsProperties(t, f, false)

	testUtils.CheckResourcesExistence(
		true, true, f.portClient, t,
		[]string{blueprintId}, []string{},
		[]string{
			fmt.Sprintf("create_%s", blueprintId),
			fmt.Sprintf("update_%s", blueprintId),
			fmt.Sprintf("delete_%s", blueprintId),
		},
	)
}

func TestCRD_crd_autoDiscoverCRDsToActionsNamespaced(t *testing.T) {
	f := newFixture(t, "", true, "true")
	blueprintId := getBlueprintId(f.stateKey)

	AutodiscoverCRDsToActions(f.portConfig, f.apiextensionClient, f.portClient)

	checkBlueprintAndActionsProperties(t, f, true)

	testUtils.CheckResourcesExistence(
		true, true, f.portClient, t,
		[]string{blueprintId}, []string{},
		[]string{
			fmt.Sprintf("create_%s", blueprintId),
			fmt.Sprintf("update_%s", blueprintId),
			fmt.Sprintf("delete_%s", blueprintId),
		},
	)
}

func TestCRD_crd_autoDiscoverCRDsToActionsNoCRDs(t *testing.T) {
	f := newFixture(t, "", false, "false")
	blueprintId := getBlueprintId(f.stateKey)

	AutodiscoverCRDsToActions(f.portConfig, f.apiextensionClient, f.portClient)

	testUtils.CheckResourcesExistence(
		false, false, f.portClient, t,
		[]string{blueprintId}, []string{},
		[]string{
			fmt.Sprintf("create_%s", blueprintId),
			fmt.Sprintf("update_%s", blueprintId),
			fmt.Sprintf("delete_%s", blueprintId),
		},
	)
}
