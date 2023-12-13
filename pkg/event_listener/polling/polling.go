package polling

import (
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/integration"
	"k8s.io/klog/v2"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"
)

type PollingHandler struct {
	ticker      *time.Ticker
	stateKey    string
	portClient  *cli.PortClient
	pollingRate int
}

func NewPollingHandler(pollingRate int, stateKey string, portClient *cli.PortClient) *PollingHandler {
	rv := &PollingHandler{
		ticker:      time.NewTicker(time.Second * time.Duration(pollingRate)),
		stateKey:    stateKey,
		portClient:  portClient,
		pollingRate: pollingRate,
	}
	return rv
}

func (h *PollingHandler) Run(resync func()) {
	klog.Infof("Starting polling handler")
	currentState, err := integration.GetIntegrationConfig(h.portClient, h.stateKey)
	if err != nil {
		klog.Errorf("Error fetching the first AppConfig state: %s", err.Error())
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	klog.Infof("Polling handler started")
	run := true
	for run {
		select {
		case sig := <-sigChan:
			klog.Infof("Received signal %v: terminating\n", sig)
			run = false
		case <-h.ticker.C:
			klog.Infof("Polling event listener iteration after %d seconds. Checking for changes...", h.pollingRate)
			configuration, err := integration.GetIntegrationConfig(h.portClient, h.stateKey)
			if err != nil {
				klog.Errorf("error resyncing: %s", err.Error())
			}

			if reflect.DeepEqual(currentState, configuration) != true {
				klog.Infof("Changes detected. Resyncing...")
				currentState = configuration
				resync()
			}
		}
	}
}
