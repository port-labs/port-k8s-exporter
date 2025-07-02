package consumer

import (
	"encoding/json"
	"fmt"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/kafka_credentials"
	"github.com/port-labs/port-k8s-exporter/pkg/port/org_details"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
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

func NewEventListener(stateKey string, portClient *cli.PortClient) (*EventListener, error) {
	logger.Info("Getting Consumer Information")
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
		GroupID:                 orgId + ".k8s." + stateKey,
	}

	topic := orgId + ".change.log"
	instance, err := NewConsumer(c, nil)
	if err != nil {
		return nil, err
	}

	return &EventListener{
		stateKey:   stateKey,
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
	logger.Info("Starting Kafka event listener")

	logger.Infow("Starting consumer for topic", "topic", l.topic)
	l.consumer.Consume(l.topic, func(value []byte) {
		incomingMessage := &IncomingMessage{}
		parsingError := json.Unmarshal(value, &incomingMessage)
		if parsingError != nil {
			logger.Errorw("error handling message", "error", parsingError.Error())
			utilruntime.HandleError(fmt.Errorf("error handling message: %s", parsingError.Error()))
		} else if shouldResync(l.stateKey, incomingMessage) {
			logger.Info("Changes detected. Resyncing...")
			resync()
		}
	}, nil)

	return nil
}
