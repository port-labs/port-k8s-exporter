package polling

import (
	"flag"
	"fmt"
	guuid "github.com/google/uuid"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
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
	config.Init()
	flag.Parse()
	stateKey := guuid.NewString()
	portClient, err := cli.New("https://api.getport.io", cli.WithHeader("User-Agent", fmt.Sprintf("port-k8s-exporter/0.1 (statekey/%s)", stateKey)),
		cli.WithClientID(config.ApplicationConfig.PortClientId), cli.WithClientSecret(config.ApplicationConfig.PortClientSecret))
	if err != nil {
		t.Errorf("Error building Port client: %s", err.Error())
	}

	_ = integration.DeleteIntegration(portClient, stateKey)
	err = integration.NewIntegration(portClient, stateKey, "", []port.Resource{})
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
	time.Sleep(time.Millisecond * 500)
	assert.False(t, called)

	_ = integration.UpdateIntegrationConfig(fixture.portClient, fixture.stateKey, &port.AppConfig{
		Resources: []port.Resource{},
	})

	c <- time.Now()
	time.Sleep(time.Millisecond * 500)

	assert.True(t, called)
}
