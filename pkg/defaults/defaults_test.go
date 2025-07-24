package defaults

import (
	"testing"

	guuid "github.com/google/uuid"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/blueprint"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"github.com/port-labs/port-k8s-exporter/pkg/port/page"
	testUtils "github.com/port-labs/port-k8s-exporter/test_utils"
	"github.com/stretchr/testify/assert"
)

type Fixture struct {
	t          *testing.T
	portClient *cli.PortClient
	stateKey   string
}

func tearDownFixture(
	t *testing.T,
	f *Fixture,
) {
	t.Logf("Deleting default resources for %s", f.stateKey)
	deleteDefaultResources(f.portClient, f.stateKey)
}

func NewFixture(t *testing.T) *Fixture {
	stateKey := guuid.NewString()
	portClient := cli.New(config.ApplicationConfig)

	deleteDefaultResources(portClient, stateKey)
	return &Fixture{
		t:          t,
		portClient: portClient,
		stateKey:   stateKey,
	}
}

func (f *Fixture) CreateIntegration() {
	_, err := integration.CreateIntegration(f.portClient, f.stateKey, "", &port.IntegrationAppConfig{
		Resources: []port.Resource{},
	}, false, "unknown")

	if err != nil {
		f.t.Errorf("Error creating Port integration: %s", err.Error())
	}
}

func (f *Fixture) CleanIntegration() {
	_ = integration.DeleteIntegration(f.portClient, f.stateKey)
}

func deleteDefaultResources(portClient *cli.PortClient, stateKey string) {
	_ = integration.DeleteIntegration(portClient, stateKey)
	_ = blueprint.DeleteBlueprintEntities(portClient, "workload")
	_ = blueprint.DeleteBlueprint(portClient, "workload")
	_ = blueprint.DeleteBlueprintEntities(portClient, "namespace")
	_ = blueprint.DeleteBlueprint(portClient, "namespace")
	_ = blueprint.DeleteBlueprintEntities(portClient, "cluster")
	_ = blueprint.DeleteBlueprint(portClient, "cluster")
	_ = page.DeletePage(portClient, "workload_overview_dashboard")
	_ = page.DeletePage(portClient, "availability_scorecard_dashboard")
}

func Test_InitIntegration_InitDefaults(t *testing.T) {
	f := NewFixture(t)
	defer tearDownFixture(t, f)
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:                  f.stateKey,
		EventListenerType:         "POLLING",
		CreateDefaultResources:    true,
		CreatePortResourcesOrigin: port.CreatePortResourcesOriginK8S,
	}, "unknown", true)
	assert.Nil(t, e)

	_, err := integration.GetIntegration(f.portClient, f.stateKey)
	assert.Nil(t, err)

	_, err = blueprint.GetBlueprint(f.portClient, "workload")
	assert.Nil(t, err)

	_, err = blueprint.GetBlueprint(f.portClient, "namespace")
	assert.Nil(t, err)

	_, err = blueprint.GetBlueprint(f.portClient, "cluster")
	assert.Nil(t, err)

	_, err = page.GetPage(f.portClient, "workload_overview_dashboard")
	assert.Nil(t, err)

	_, err = page.GetPage(f.portClient, "availability_scorecard_dashboard")
	assert.Nil(t, err)
}

func Test_InitIntegration_InitDefaults_CreateDefaultResources_False(t *testing.T) {
	f := NewFixture(t)
	defer tearDownFixture(t, f)
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:                  f.stateKey,
		EventListenerType:         "POLLING",
		CreateDefaultResources:    false,
		CreatePortResourcesOrigin: port.CreatePortResourcesOriginK8S,
	}, "unknown", true)
	assert.Nil(t, e)

	_, err := integration.GetIntegration(f.portClient, f.stateKey)
	assert.Nil(t, err)

	testUtils.CheckResourcesExistence(false, false, f.portClient, f.t, []string{"workload", "namespace", "cluster"}, []string{"workload_overview_dashboard", "availability_scorecard_dashboard"}, []string{})
}

func Test_InitIntegration_BlueprintExists(t *testing.T) {
	f := NewFixture(t)
	defer tearDownFixture(t, f)
	if _, err := blueprint.NewBlueprint(f.portClient, port.Blueprint{
		Identifier: "workload",
		Title:      "Workload",
		Schema: port.BlueprintSchema{
			Properties: map[string]port.Property{},
		},
	}); err != nil {
		t.Errorf("Error creating Port blueprint: %s", err.Error())
	}
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:                  f.stateKey,
		EventListenerType:         "POLLING",
		CreateDefaultResources:    true,
		CreatePortResourcesOrigin: port.CreatePortResourcesOriginK8S,
	}, "unknown", true)
	assert.Nil(t, e)

	i, err := integration.GetIntegration(f.portClient, f.stateKey)
	assert.NotNil(t, i.Config.Resources)
	assert.Nil(t, err)

	_, err = blueprint.GetBlueprint(f.portClient, "workload")
	assert.Nil(t, err)

	testUtils.CheckResourcesExistence(false, false, f.portClient, f.t, []string{"namespace", "cluster"}, []string{"workload_overview_dashboard", "availability_scorecard_dashboard"}, []string{})
}

func Test_InitIntegration_PageExists(t *testing.T) {
	f := NewFixture(t)
	defer tearDownFixture(t, f)
	if err := page.CreatePage(f.portClient, port.Page{
		Identifier: "workload_overview_dashboard",
		Title:      "Workload Overview Dashboard",
	}); err != nil {
		t.Errorf("Error creating Port page: %s", err.Error())
	}
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:                  f.stateKey,
		EventListenerType:         "POLLING",
		CreateDefaultResources:    true,
		CreatePortResourcesOrigin: port.CreatePortResourcesOriginK8S,
	}, "unknown", true)
	assert.Nil(t, e)

	i, err := integration.GetIntegration(f.portClient, f.stateKey)
	assert.NotNil(t, i.Config.Resources)
	assert.Nil(t, err)

	_, err = page.GetPage(f.portClient, "workload_overview_dashboard")
	assert.Nil(t, err)

	testUtils.CheckResourcesExistence(true, false, f.portClient, f.t, []string{"workload", "namespace", "cluster"}, []string{"availability_scorecard_dashboard"}, []string{})
}

func Test_InitIntegration_ExistingIntegration(t *testing.T) {
	f := NewFixture(t)
	defer tearDownFixture(t, f)
	_, err := integration.CreateIntegration(f.portClient, f.stateKey, "", nil, false, "unknown")
	if err != nil {
		t.Errorf("Error creating Port integration: %s", err.Error())
	}
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:                  f.stateKey,
		EventListenerType:         "POLLING",
		CreateDefaultResources:    true,
		CreatePortResourcesOrigin: port.CreatePortResourcesOriginK8S,
	}, "unknown", true)
	assert.Nil(t, e)

	_, err = integration.GetIntegration(f.portClient, f.stateKey)
	assert.Nil(t, err)

	testUtils.CheckResourcesExistence(true, false, f.portClient, f.t, []string{"workload", "namespace", "cluster"}, []string{"workload_overview_dashboard", "availability_scorecard_dashboard"}, []string{})
}

func Test_InitIntegration_LocalResourcesConfiguration(t *testing.T) {
	f := NewFixture(t)
	defer tearDownFixture(t, f)
	_, err := integration.CreateIntegration(f.portClient, f.stateKey, "", nil, false, "unknown")
	if err != nil {
		t.Errorf("Error creating Port integration: %s", err.Error())
	}
	expectedResources := []port.Resource{
		{
			Kind: "workload",
			Port: port.Port{
				Entity: port.EntityMappings{
					Mappings: []port.EntityMapping{
						{
							Identifier: "\"workload\"",
							Title:      "\"Workload\"",
							Blueprint:  "\"workload\"",
							Icon:       "\"Microservice\"",
							Properties: map[string]string{
								"namespace": "\"default\"",
							},
						},
					},
				},
			},
		},
	}
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:                  f.stateKey,
		EventListenerType:         "POLLING",
		Resources:                 expectedResources,
		CreateDefaultResources:    true,
		CreatePortResourcesOrigin: port.CreatePortResourcesOriginK8S,
	}, "unknown", true)
	assert.Nil(t, e)

	i, err := integration.GetIntegration(f.portClient, f.stateKey)
	assert.Equal(t, expectedResources, i.Config.Resources)
	assert.Nil(t, err)

	testUtils.CheckResourcesExistence(true, false, f.portClient, f.t, []string{"workload", "namespace", "cluster"}, []string{"workload_overview_dashboard", "availability_scorecard_dashboard"}, []string{})
}

func Test_InitIntegration_LocalResourcesConfiguration_ExistingIntegration_EmptyConfiguration(t *testing.T) {
	f := NewFixture(t)
	defer tearDownFixture(t, f)
	_, err := integration.CreateIntegration(f.portClient, f.stateKey, "POLLING", nil, false, "unknown")
	if err != nil {
		t.Errorf("Error creating Port integration: %s", err.Error())
	}
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:                  f.stateKey,
		EventListenerType:         "KAFKA",
		Resources:                 nil,
		CreateDefaultResources:    true,
		CreatePortResourcesOrigin: port.CreatePortResourcesOriginK8S,
	}, "unknown", true)
	assert.Nil(t, e)

	i, err := integration.GetIntegration(f.portClient, f.stateKey)
	assert.Nil(t, err)
	assert.Equal(t, "KAFKA", i.EventListener.Type)

	testUtils.CheckResourcesExistence(true, false, f.portClient, f.t, []string{"workload", "namespace", "cluster"}, []string{"workload_overview_dashboard", "availability_scorecard_dashboard"}, []string{})
}

func Test_InitIntegration_LocalResourcesConfiguration_ExistingIntegration_WithConfiguration_WithOverwriteConfigurationOnRestartFlag(t *testing.T) {
	f := NewFixture(t)
	defer tearDownFixture(t, f)

	expectedConfig := &port.IntegrationAppConfig{
		Resources: []port.Resource{
			{
				Kind: "workload",
				Port: port.Port{
					Entity: port.EntityMappings{
						Mappings: []port.EntityMapping{
							{
								Identifier: "\"workload\"",
								Title:      "\"Workload\"",
								Blueprint:  "\"workload\"",
								Icon:       "\"Microservice\"",
								Properties: map[string]string{
									"namespace": "\"default\"",
								},
							},
						},
					},
				},
			},
		},
	}
	_, err := integration.CreateIntegration(f.portClient, f.stateKey, "POLLING", expectedConfig, false, "unknown")
	if err != nil {
		t.Errorf("Error creating Port integration: %s", err.Error())
	}

	expectedConfig.Resources[0].Kind = "namespace"
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:                        f.stateKey,
		EventListenerType:               "KAFKA",
		Resources:                       expectedConfig.Resources,
		CreateDefaultResources:          true,
		CreatePortResourcesOrigin:       port.CreatePortResourcesOriginK8S,
		OverwriteConfigurationOnRestart: true,
	}, "unknown", true)
	assert.Nil(t, e)

	i, err := integration.GetIntegration(f.portClient, f.stateKey)
	assert.Nil(t, err)
	assert.Equal(t, expectedConfig.Resources, i.Config.Resources)

	testUtils.CheckResourcesExistence(true, false, f.portClient, f.t, []string{"workload", "namespace", "cluster"}, []string{"workload_overview_dashboard", "availability_scorecard_dashboard"}, []string{})
}
