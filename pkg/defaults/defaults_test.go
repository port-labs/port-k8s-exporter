package defaults

import (
	"fmt"
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

func NewFixture(t *testing.T) *Fixture {
	stateKey := guuid.NewString()
	portClient, err := cli.New(config.ApplicationConfig.PortBaseURL, cli.WithHeader("User-Agent", fmt.Sprintf("port-k8s-exporter/0.1 (statekey/%s)", stateKey)),
		cli.WithClientID(config.ApplicationConfig.PortClientId), cli.WithClientSecret(config.ApplicationConfig.PortClientSecret))
	if err != nil {
		t.Errorf("Error building Port client: %s", err.Error())
	}

	deleteDefaultResources(portClient, stateKey)
	return &Fixture{
		t:          t,
		portClient: portClient,
		stateKey:   stateKey,
	}
}

func (f *Fixture) CreateIntegration() {
	err := integration.CreateIntegration(f.portClient, f.stateKey, "", &port.IntegrationAppConfig{
		Resources: []port.Resource{},
	})

	if err != nil {
		f.t.Errorf("Error creating Port integration: %s", err.Error())
	}
}

func (f *Fixture) CleanIntegration() {
	_ = integration.DeleteIntegration(f.portClient, f.stateKey)
}

func deleteDefaultResources(portClient *cli.PortClient, stateKey string) {
	_ = integration.DeleteIntegration(portClient, stateKey)
	_ = blueprint.DeleteBlueprint(portClient, "workload")
	_ = blueprint.DeleteBlueprint(portClient, "namespace")
	_ = blueprint.DeleteBlueprint(portClient, "cluster")
	_ = page.DeletePage(portClient, "workload_overview_dashboard")
	_ = page.DeletePage(portClient, "availability_scorecard_dashboard")
}

func Test_InitIntegration_InitDefaults(t *testing.T) {
	f := NewFixture(t)
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:               f.stateKey,
		EventListenerType:      "POLLING",
		CreateDefaultResources: true,
	})
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
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:               f.stateKey,
		EventListenerType:      "POLLING",
		CreateDefaultResources: false,
	})
	assert.Nil(t, e)

	_, err := integration.GetIntegration(f.portClient, f.stateKey)
	assert.Nil(t, err)

	testUtils.CheckResourcesExistence(false, f.portClient, f.t, []string{"workload", "namespace", "cluster"}, []string{"workload_overview_dashboard", "availability_scorecard_dashboard"}, []string{})
}

func Test_InitIntegration_BlueprintExists(t *testing.T) {
	f := NewFixture(t)
	if _, err := blueprint.NewBlueprint(f.portClient, port.Blueprint{
		Identifier: "workload",
		Title:      "Workload",
		Schema: port.Schema{
			Properties: map[string]port.Property{},
		},
	}); err != nil {
		t.Errorf("Error creating Port blueprint: %s", err.Error())
	}
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:               f.stateKey,
		EventListenerType:      "POLLING",
		CreateDefaultResources: true,
	})
	assert.Nil(t, e)

	i, err := integration.GetIntegration(f.portClient, f.stateKey)
	assert.Nil(t, i.Config.Resources)
	assert.Nil(t, err)

	_, err = blueprint.GetBlueprint(f.portClient, "workload")
	assert.Nil(t, err)

	testUtils.CheckResourcesExistence(false, f.portClient, f.t, []string{"namespace", "cluster"}, []string{"workload_overview_dashboard", "availability_scorecard_dashboard"}, []string{})
}

func Test_InitIntegration_PageExists(t *testing.T) {
	f := NewFixture(t)
	if err := page.CreatePage(f.portClient, port.Page{
		Identifier: "workload_overview_dashboard",
		Title:      "Workload Overview Dashboard",
	}); err != nil {
		t.Errorf("Error creating Port page: %s", err.Error())
	}
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:               f.stateKey,
		EventListenerType:      "POLLING",
		CreateDefaultResources: true,
	})
	assert.Nil(t, e)

	i, err := integration.GetIntegration(f.portClient, f.stateKey)
	assert.Nil(t, i.Config.Resources)
	assert.Nil(t, err)

	_, err = page.GetPage(f.portClient, "workload_overview_dashboard")
	assert.Nil(t, err)

	testUtils.CheckResourcesExistence(false, f.portClient, f.t, []string{"workload", "namespace", "cluster"}, []string{"availability_scorecard_dashboard"}, []string{})
}

func Test_InitIntegration_ExistingIntegration(t *testing.T) {
	f := NewFixture(t)
	err := integration.CreateIntegration(f.portClient, f.stateKey, "", nil)
	if err != nil {
		t.Errorf("Error creating Port integration: %s", err.Error())
	}
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:               f.stateKey,
		EventListenerType:      "POLLING",
		CreateDefaultResources: true,
	})
	assert.Nil(t, e)

	_, err = integration.GetIntegration(f.portClient, f.stateKey)
	assert.Nil(t, err)

	testUtils.CheckResourcesExistence(false, f.portClient, f.t, []string{"workload", "namespace", "cluster"}, []string{"workload_overview_dashboard", "availability_scorecard_dashboard"}, []string{})
}

func Test_InitIntegration_LocalResourcesConfiguration(t *testing.T) {
	f := NewFixture(t)
	err := integration.CreateIntegration(f.portClient, f.stateKey, "", nil)
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
		StateKey:               f.stateKey,
		EventListenerType:      "POLLING",
		Resources:              expectedResources,
		CreateDefaultResources: true,
	})
	assert.Nil(t, e)

	i, err := integration.GetIntegration(f.portClient, f.stateKey)
	assert.Equal(t, expectedResources, i.Config.Resources)
	assert.Nil(t, err)

	testUtils.CheckResourcesExistence(false, f.portClient, f.t, []string{"workload", "namespace", "cluster"}, []string{"workload_overview_dashboard", "availability_scorecard_dashboard"}, []string{})
}

func Test_InitIntegration_LocalResourcesConfiguration_ExistingIntegration_EmptyConfiguration(t *testing.T) {
	f := NewFixture(t)
	err := integration.CreateIntegration(f.portClient, f.stateKey, "POLLING", nil)
	if err != nil {
		t.Errorf("Error creating Port integration: %s", err.Error())
	}
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:               f.stateKey,
		EventListenerType:      "KAFKA",
		Resources:              nil,
		CreateDefaultResources: true,
	})
	assert.Nil(t, e)

	i, err := integration.GetIntegration(f.portClient, f.stateKey)
	assert.Nil(t, err)
	assert.Equal(t, "KAFKA", i.EventListener.Type)

	testUtils.CheckResourcesExistence(false, f.portClient, f.t, []string{"workload", "namespace", "cluster"}, []string{"workload_overview_dashboard", "availability_scorecard_dashboard"}, []string{})
}

func Test_InitIntegration_LocalResourcesConfiguration_ExistingIntegration_WithConfiguration_WithOverwriteConfigurationOnRestartFlag(t *testing.T) {
	f := NewFixture(t)
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
	err := integration.CreateIntegration(f.portClient, f.stateKey, "POLLING", expectedConfig)
	if err != nil {
		t.Errorf("Error creating Port integration: %s", err.Error())
	}

	expectedConfig.Resources[0].Kind = "namespace"
	e := InitIntegration(f.portClient, &port.Config{
		StateKey:                        f.stateKey,
		EventListenerType:               "KAFKA",
		Resources:                       expectedConfig.Resources,
		CreateDefaultResources:          true,
		OverwriteConfigurationOnRestart: true,
	})
	assert.Nil(t, e)

	i, err := integration.GetIntegration(f.portClient, f.stateKey)
	assert.Nil(t, err)
	assert.Equal(t, expectedConfig.Resources, i.Config.Resources)

	testUtils.CheckResourcesExistence(false, f.portClient, f.t, []string{"workload", "namespace", "cluster"}, []string{"workload_overview_dashboard", "availability_scorecard_dashboard"}, []string{})
}
