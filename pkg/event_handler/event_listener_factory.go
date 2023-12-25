package event_handler

import (
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/event_handler/consumer"
	"github.com/port-labs/port-k8s-exporter/pkg/event_handler/polling"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"k8s.io/klog/v2"
)

func CreateEventListener(stateKey string, eventListenerType string, portClient *cli.PortClient) (IListener, error) {
	klog.Infof("Received event listener type: %s", eventListenerType)
	switch eventListenerType {
	case "KAFKA":
		return consumer.NewEventListener(stateKey, portClient)
	case "POLLING":
		return polling.NewEventListener(stateKey, portClient), nil
	default:
		return nil, fmt.Errorf("unknown event listener type: %s", eventListenerType)
	}
}
