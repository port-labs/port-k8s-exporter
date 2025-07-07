package polling

import (
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
)

type ITicker interface {
	GetC() <-chan time.Time
}

type Ticker struct {
	ticker *time.Ticker
}

func NewTicker(d time.Duration) *Ticker {
	return &Ticker{
		ticker: time.NewTicker(d),
	}
}

func (t *Ticker) Stop() {
	t.ticker.Stop()
}

func (t *Ticker) GetC() <-chan time.Time {
	return t.ticker.C
}

type Handler struct {
	ticker      ITicker
	stateKey    string
	portClient  *cli.PortClient
	pollingRate uint
}

func NewPollingHandler(pollingRate uint, stateKey string, portClient *cli.PortClient, tickerOverride ITicker) *Handler {
	ticker := tickerOverride
	if ticker == nil {
		ticker = NewTicker(time.Second * time.Duration(pollingRate))
	}
	rv := &Handler{
		ticker:      ticker,
		stateKey:    stateKey,
		portClient:  portClient,
		pollingRate: pollingRate,
	}
	return rv
}

func (h *Handler) Run(resync func()) {
	logger.Infof("Starting polling handler")
	currentState, err := integration.GetIntegration(h.portClient, h.stateKey)
	if err != nil {
		logger.Errorf("Error fetching the first AppConfig state: %s", err.Error())
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logger.Infof("Polling handler started")
	run := true
	for run {
		select {
		case sig := <-sigChan:
			logger.Infof("Received signal %v: terminating\n", sig)
			// Flush any pending logs before termination
			logger.Shutdown()
			run = false
		case <-h.ticker.GetC():
			logger.Infof("Polling event listener iteration after %d seconds. Checking for changes...", h.pollingRate)
			configuration, err := integration.GetIntegration(h.portClient, h.stateKey)
			if err != nil {
				logger.Errorf("error getting integration: %s", err.Error())
			} else if reflect.DeepEqual(currentState, configuration) != true {
				logger.Infof("Changes detected. Resyncing...")
				currentState = configuration
				resync()
			}
		}
	}
}
