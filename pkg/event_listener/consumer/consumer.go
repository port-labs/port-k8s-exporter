package consumer

import (
	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"k8s.io/klog/v2"
	"os"
	"os/signal"
	"syscall"
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

func NewConsumer(config *config.KafkaConfiguration, overrideConsumer IConsume) (*Consumer, error) {
	c := overrideConsumer
	var err error
	if overrideConsumer == nil {
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
			klog.Infof("partition(s) assigned: %v\n", ev.Partitions)
			if readyChan != nil && ready == false {
				close(readyChan)
				ready = true
			}
		}

		return nil
	}

	if err := c.client.SubscribeTopics(topics, rebalanceCallback); err != nil {
		klog.Fatalf("Error subscribing to topic: %s", err.Error())
	}
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	klog.Infof("Waiting for assigned partitions...")

	go func() {
		sig := <-sigChan
		klog.Infof("Caught signal %v: terminating\n", sig)
		c.Close()
	}()

	for c.enabled {
		ev := c.client.Poll(100)
		if ev == nil {
			continue
		}

		switch e := ev.(type) {
		case *kafka.Message:
			klog.Infof("%% New event\n%s\n",
				string(e.Value))

			handler(e.Value)
			if _, err := c.client.Commit(); err != nil {
				klog.Error("Error committing offset: %s", err.Error())
			}
		case kafka.Error:
			klog.Infof("%% Error: %v\n", e)
		default:
			klog.Infof("Ignored %v\n", e)
		}
	}
	klog.Infof("Closed consumer\n")
}

func (c *Consumer) Close() {
	if err := c.client.Close(); err != nil {
		klog.Fatalf("Error closing consumer: %s", err.Error())
	}
	c.enabled = false
}
