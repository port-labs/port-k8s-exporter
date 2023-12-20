package polling

import (
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"k8s.io/klog/v2"
)

type EventListener struct {
	stateKey   string
	portClient *cli.PortClient
	handler    *Handler
}

func NewEventListener(stateKey string, portClient *cli.PortClient) *EventListener {
	return &EventListener{
		stateKey:   stateKey,
		portClient: portClient,
		handler:    NewPollingHandler(config.PollingListenerRate, stateKey, portClient, nil),
	}
}

func (l *EventListener) Run(resync func()) error {
	klog.Infof("Starting polling event listener")
	klog.Infof("Polling rate set to %d seconds", config.PollingListenerRate)
	return l.handler.Run(resync)
}
