package consumer

import (
	"encoding/json"
	"fmt"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/kafka_credentials"
	"github.com/port-labs/port-k8s-exporter/pkg/port/org_details"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
)

type EventListener struct {
	stateKey   string
	portClient *cli.PortClient
	topic      string
	consumer   *Consumer
}

type IncomingMessage struct {
	Diff *struct {
		After *struct {
			Identifier string `json:"installationId"`
		} `json:"after"`
	} `json:"diff"`
}

func NewEventListener(portClient *cli.PortClient) (*EventListener, error) {
	klog.Infof("Getting Consumer Information")
	credentials, err := kafka_credentials.GetKafkaCredentials(portClient)
	if err != nil {
		return nil, err
	}
	orgId, err := org_details.GetOrgId(portClient)
	if err != nil {
		return nil, err
	}

	c := &config.KafkaConfiguration{
		Brokers:                 config.KafkaConfig.Brokers,
		SecurityProtocol:        config.KafkaConfig.SecurityProtocol,
		AuthenticationMechanism: config.KafkaConfig.AuthenticationMechanism,
		Username:                credentials.Username,
		Password:                credentials.Password,
		GroupID:                 orgId + ".k8s." + config.ApplicationConfig.StateKey,
	}

	topic := orgId + ".change.log"
	instance, err := NewConsumer(c, nil)
	if err != nil {
		return nil, err
	}

	return &EventListener{
		stateKey:   config.ApplicationConfig.StateKey,
		portClient: portClient,
		topic:      topic,
		consumer:   instance,
	}, nil
}

func shouldResync(stateKey string, message *IncomingMessage) bool {
	return message.Diff != nil &&
		message.Diff.After != nil &&
		message.Diff.After.Identifier != "" &&
		message.Diff.After.Identifier == stateKey
}

func (l *EventListener) Run(resync func()) error {
	klog.Infof("Starting Kafka event listener")

	klog.Infof("Starting consumer for topic %s", l.topic)
	l.consumer.Consume(l.topic, func(value []byte) {
		incomingMessage := &IncomingMessage{}
		parsingError := json.Unmarshal(value, &incomingMessage)
		if parsingError != nil {
			utilruntime.HandleError(fmt.Errorf("error handling message: %s", parsingError.Error()))
		} else if shouldResync(l.stateKey, incomingMessage) {
			klog.Infof("Changes detected. Resyncing...")
			resync()
		}
	}, nil)

	return nil
}
