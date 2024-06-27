package event_handler

import (
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/event_handler/consumer"
	"github.com/port-labs/port-k8s-exporter/pkg/event_handler/polling"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"k8s.io/klog/v2"
)

func CreateEventListener(stateKey string, eventListenerType string) (IListener, error) {
	portClient, err := cli.New()
	if err != nil {
		return nil, fmt.Errorf("error building Port client: %v", err)
	}

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
