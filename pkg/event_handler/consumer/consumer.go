package consumer

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
)

type IConsume interface {
	SubscribeTopics(topics []string, rebalanceCb kafka.RebalanceCb) (err error)
	Poll(timeoutMs int) (event kafka.Event)
	Commit() (offsets []kafka.TopicPartition, err error)
	Close() (err error)
}

type Consumer struct {
	client  IConsume
	enabled bool
}

type JsonHandler func(value []byte)

func NewConsumer(config *config.KafkaConfiguration, overrideKafkaConsumer IConsume) (*Consumer, error) {
	c := overrideKafkaConsumer
	var err error
	if overrideKafkaConsumer == nil {
		c, err = kafka.NewConsumer(&kafka.ConfigMap{
			"bootstrap.servers": config.Brokers,
			"client.id":         "port-k8s-exporter",
			"group.id":          config.GroupID,
			"security.protocol": config.SecurityProtocol,
			"sasl.mechanism":    config.AuthenticationMechanism,
			"sasl.username":     config.Username,
			"sasl.password":     config.Password,
			"auto.offset.reset": "latest",
		})

		if err != nil {
			return nil, err
		}
	}

	return &Consumer{client: c, enabled: true}, nil
}

func (c *Consumer) Consume(topic string, handler JsonHandler, readyChan chan bool) {
	topics := []string{topic}
	ready := false

	rebalanceCallback := func(c *kafka.Consumer, event kafka.Event) error {
		switch ev := event.(type) {
		case kafka.AssignedPartitions:
			logger.Infof("partition(s) assigned: %v\n", ev.Partitions)
			if readyChan != nil && ready == false {
				close(readyChan)
				ready = true
			}
		}

		return nil
	}

	if err := c.client.SubscribeTopics(topics, rebalanceCallback); err != nil {
		logger.Fatalf("Error subscribing to topic: %s", err.Error())
	}
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logger.Infof("Waiting for assigned partitions...")

	go func() {
		sig := <-sigChan
		logger.Infof("Caught signal %v: terminating\n", sig)
		// Flush any pending logs before closing consumer
		logger.Shutdown()
		c.Close()
	}()

	for c.enabled {
		ev := c.client.Poll(100)
		if ev == nil {
			continue
		}

		switch e := ev.(type) {
		case *kafka.Message:
			logger.Infof("%% New event\n%s\n",
				string(e.Value))

			handler(e.Value)
			if _, err := c.client.Commit(); err != nil {
				logger.Errorf("Error committing offset: %s", err.Error())
			}
		case kafka.Error:
			logger.Infof("%% Error: %v\n", e)
		default:
			logger.Infof("Ignored %v\n", e)
		}
	}
	logger.Infof("Closed consumer\n")
}

func (c *Consumer) Close() {
	if err := c.client.Close(); err != nil {
		logger.Fatalf("Error closing consumer: %s", err.Error())
	}
	c.enabled = false
}
