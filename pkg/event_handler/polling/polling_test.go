package polling

import (
	_ "github.com/port-labs/port-k8s-exporter/test_utils"

	"testing"
	"time"

	guuid "github.com/google/uuid"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"github.com/stretchr/testify/assert"
)

type Fixture struct {
	t          *testing.T
	ticker     MockTicker
	portClient *cli.PortClient
	stateKey   string
}

type MockTicker struct {
	c chan time.Time
}

func (m *MockTicker) GetC() <-chan time.Time {
	return m.c
}

func NewFixture(t *testing.T, c chan time.Time) *Fixture {
	stateKey := guuid.NewString()
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

	portClient := cli.New(newConfig)
	_ = integration.DeleteIntegration(portClient, stateKey)
	_, err := integration.CreateIntegration(portClient, stateKey, "", &port.IntegrationAppConfig{
		Resources: []port.Resource{},
	}, false)
	if err != nil {
		t.Errorf("Error creating Port integration: %s", err.Error())
	}
	return &Fixture{
		t:          t,
		ticker:     MockTicker{c: c},
		portClient: portClient,
		stateKey:   stateKey,
	}
}

func (f *Fixture) CleanIntegration() {
	_ = integration.DeleteIntegration(f.portClient, f.stateKey)
}

func TestPolling_DifferentConfiguration(t *testing.T) {
	called := false
	c := make(chan time.Time)
	fixture := NewFixture(t, c)
	defer fixture.CleanIntegration()
	handler := NewPollingHandler(uint(1), fixture.stateKey, fixture.portClient, &fixture.ticker)
	go handler.Run(func() {
		called = true
	})

	c <- time.Now()
	time.Sleep(time.Millisecond * 1500)
	assert.False(t, called)

	_ = integration.PatchIntegration(fixture.portClient, fixture.stateKey, &port.Integration{
		Config: &port.IntegrationAppConfig{
			Resources: []port.Resource{},
		},
	})

	c <- time.Now()
	time.Sleep(time.Millisecond * 1500)

	assert.True(t, called)
}
