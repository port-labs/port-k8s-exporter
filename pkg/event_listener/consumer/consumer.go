package consumer

import (
	"github.com/confluentinc/confluent-kafka-go/kafka"
	"k8s.io/klog/v2"
	"os"
	"os/signal"
	"syscall"
)

type Consumer struct {
	client *kafka.Consumer
}

type KafkaConfiguration struct {
	Brokers                 string
	SecurityProtocol        string
	GroupID                 string
	AuthenticationMechanism string
	Username                string
	Password                string
	KafkaSecurityEnabled    bool
}

type JsonHandler func(value []byte)

func NewConsumer(config *KafkaConfiguration) (*Consumer, error) {
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
	_ = c.client.SubscribeTopics(topics, nil)
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)

	run := true
	for run {
		select {
		case sig := <-sigchan:
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