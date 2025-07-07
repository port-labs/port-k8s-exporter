package polling

import (
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
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
	logger.Infof("Starting polling event listener")
	logger.Infof("Polling rate set to %d seconds", config.PollingListenerRate)
	l.handler.Run(resync)

	return nil
}
