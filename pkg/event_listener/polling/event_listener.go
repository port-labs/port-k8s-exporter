package polling

import (
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"k8s.io/klog/v2"
)

type EventListener struct {
	stateKey   string
	portClient *cli.PortClient
}

func NewEventListener(portClient *cli.PortClient) *EventListener {
	return &EventListener{
		stateKey:   config.ApplicationConfig.StateKey,
		portClient: portClient,
	}
}

func (l *EventListener) Run(resync func()) error {
	klog.Infof("Starting polling event listener")
	klog.Infof("Polling rate set to %d seconds", config.PollingListenerRate)
	pollingHandler := NewPollingHandler(config.PollingListenerRate, l.stateKey, l.portClient, nil)
	pollingHandler.Run(resync)

	return nil
}
