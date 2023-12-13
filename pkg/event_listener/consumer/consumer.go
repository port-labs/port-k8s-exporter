package consumer

import (
	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"k8s.io/klog/v2"
	"os"
	"os/signal"
	"syscall"
)

type Consumer struct {
	client *kafka.Consumer
}

type JsonHandler func(value []byte)

func NewConsumer(config *config.KafkaConfiguration) (*Consumer, error) {
	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": config.Brokers,
		"group.id":          config.GroupID,
		"security.protocol": config.SecurityProtocol,
		"sasl.mechanism":    config.AuthenticationMechanism,
		"sasl.username":     config.Username,
		"sasl.password":     config.Password,
		"auto.offset.reset": "earliest",
	})

	if err != nil {
		return nil, err
	}

	return &Consumer{client: c}, nil
}

func (c *Consumer) Consume(topic string, handler JsonHandler) {
	topics := []string{topic}
	err := c.client.SubscribeTopics(topics, nil)
	if err != nil {
		klog.Fatalf("Error subscribing to topic: %s", err.Error())
	}
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	run := true
	for run {
		select {
		case sig := <-sigChan:
			klog.Infof("Caught signal %v: terminating\n", sig)
			run = false
		default:
			ev := c.client.Poll(100)
			if ev == nil {
				continue
			}

			switch e := ev.(type) {
			case *kafka.Message:
				klog.Infof("%% Message on %s:\n%s\n",
					e.TopicPartition, string(e.Value))

				handler(e.Value)
			case *kafka.AssignedPartitions:
				klog.Infof("AssignedPartitions: %v\n", e)
			case kafka.Error:
				klog.Infof("%% Error: %v\n", e)
			default:
				klog.Infof("Ignored %v\n", e)
			}
		}
	}
}
