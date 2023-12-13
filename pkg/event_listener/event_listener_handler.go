package event_listener

import (
	"encoding/json"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/event_listener/consumer"
	"github.com/port-labs/port-k8s-exporter/pkg/event_listener/polling"
	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/handlers"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/kafka_credentials"
	"github.com/port-labs/port-k8s-exporter/pkg/port/org_details"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
)

type IncomingMessage struct {
	Diff *struct {
		After *struct {
			Identifier string `json:"installationId"`
		} `json:"after"`
	} `json:"diff"`
}

var kafkaConfig = &consumer.KafkaConfiguration{
	Brokers:                 config.NewString("event-listener-brokers", "localhost:9092", "Kafka brokers"),
	SecurityProtocol:        config.NewString("event-listener-security-protocol", "plaintext", "Kafka security protocol"),
	AuthenticationMechanism: config.NewString("event-listener-authentication-mechanism", "none", "Kafka authentication mechanism"),
}

type EventListener struct {
	settings          port.EventListenerSettings
	stateKey          string
	controllerHandler *handlers.ControllersHandler
	portClient        *cli.PortClient
}

func shouldResync(stateKey string, message *IncomingMessage) bool {
	return message.Diff != nil &&
		message.Diff.After != nil &&
		message.Diff.After.Identifier != "" &&
		message.Diff.After.Identifier == stateKey
}

func NewEventListener(stateKey string, eventListenerType string, controllerHandler *handlers.ControllersHandler, client *cli.PortClient) *EventListener {
	eventListener := &EventListener{
		settings: port.EventListenerSettings{
			Type: eventListenerType,
		},
		stateKey:          stateKey,
		controllerHandler: controllerHandler,
		portClient:        client,
	}

	return eventListener
}

func startKafkaEventListener(l *EventListener, resync func()) error {
	klog.Infof("Starting Kafka event listener")
	klog.Infof("Getting Consumer Information")
	credentials, err := kafka_credentials.GetKafkaCredentials(l.portClient)
	if err != nil {
		return err
	}
	orgId, err := org_details.GetOrgId(l.portClient)
	if err != nil {
		return err
	}

	c := &consumer.KafkaConfiguration{
		Brokers:                 kafkaConfig.Brokers,
		SecurityProtocol:        kafkaConfig.SecurityProtocol,
		AuthenticationMechanism: kafkaConfig.AuthenticationMechanism,
		Username:                credentials.Username,
		Password:                credentials.Password,
		GroupID:                 orgId + ".k8s." + l.stateKey,
	}

	topic := orgId + ".change.log"
	instance, err := consumer.NewConsumer(c)

	if err != nil {
		return err
	}

	klog.Infof("Starting consumer for topic %s and groupId %s", topic, c.GroupID)
	instance.Consume(topic, func(value []byte) {
		incomingMessage := &IncomingMessage{}
		parsingError := json.Unmarshal(value, &incomingMessage)
		if parsingError != nil {
			utilruntime.HandleError(fmt.Errorf("error handling message: %s", parsingError.Error()))
		} else if shouldResync(l.stateKey, incomingMessage) {
			klog.Infof("Changes detected. Resyncing...")
			resync()
		}
	})

	return nil
}

func startPollingEventListener(l *EventListener, resync func()) {
	klog.Infof("Starting polling event listener")
	pollingRate := goutils.GetUintEnvOrDefault("EVENT_LISTENER__POLLING_RATE", 60)
	klog.Infof("Polling rate set to %d seconds", pollingRate)
	pollingHandler := polling.NewPollingHandler(pollingRate, l.stateKey, l.portClient)
	pollingHandler.Run(resync)
}

func (l *EventListener) Start(resync func(*handlers.ControllersHandler) (*handlers.ControllersHandler, error)) error {
	wrappedResync := func() {
		klog.Infof("Resync request received. Recreating controllers for the new port configuration")
		newController, resyncErr := resync(l.controllerHandler)
		l.controllerHandler = newController

		if resyncErr != nil {
			utilruntime.HandleError(fmt.Errorf("error resyncing: %s", resyncErr.Error()))
		}
	}
	klog.Infof("Received event listener type: %s", l.settings.Type)
	switch l.settings.Type {
	case "KAFKA":
		err := startKafkaEventListener(l, wrappedResync)
		if err != nil {
			return err
		}
	case "POLLING":
		startPollingEventListener(l, wrappedResync)
	default:
		return fmt.Errorf("unknown event listener type: %s", l.settings.Type)
	}

	return nil
}
