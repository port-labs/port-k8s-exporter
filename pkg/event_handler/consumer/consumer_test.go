package consumer

import (
	"testing"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/stretchr/testify/assert"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
)

type Fixture struct {
	t                 *testing.T
	mockKafkaConsumer *MockConsumer
	consumer          *Consumer
	topic             string
}

type MockConsumer struct {
	pollData kafka.Event
	close    func()
}

func (m *MockConsumer) SubscribeTopics(topics []string, rebalanceCb kafka.RebalanceCb) (err error) {
	_ = rebalanceCb(nil, kafka.AssignedPartitions{
		Partitions: []kafka.TopicPartition{
			{
				Topic:     &topics[0],
				Offset:    0,
				Partition: 0,
				Metadata:  nil,
				Error:     nil,
			},
		},
	})
	return nil
}

func (m *MockConsumer) Poll(timeoutMs int) (event kafka.Event) {
	// The consumer will poll this in while true loop so we need to close it inorder not to spam the logs
	defer func() {
		m.close()
	}()
	return m.pollData
}

func (m *MockConsumer) Commit() (offsets []kafka.TopicPartition, err error) {
	return nil, nil
}

func (m *MockConsumer) Close() (err error) {
	return nil
}

func NewFixture(t *testing.T) *Fixture {
	mock := &MockConsumer{}
	consumer, err := NewConsumer(&config.KafkaConfiguration{}, mock)
	mock.close = consumer.Close
	if err != nil {
		t.Fatalf("Error creating consumer: %v", err)
	}

	return &Fixture{
		t:                 t,
		mockKafkaConsumer: mock,
		consumer:          consumer,
		topic:             "test-topic",
	}
}

func (f *Fixture) Produce(t *testing.T, value []byte) {
	f.mockKafkaConsumer.pollData = &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &f.topic, Partition: 0},
		Value:          value,
	}
}

func (f *Fixture) Consume(handler JsonHandler) {
	readyChan := make(chan bool)
	go f.consumer.Consume(f.topic, handler, readyChan)
	<-readyChan
}

func TestConsumer_HandleJson(t *testing.T) {
	f := NewFixture(t)
	got := make(chan []byte, 1)

	f.Produce(t, []byte("test-value"))
	f.Consume(func(value []byte) { got <- value })

	select {
	case value := <-got:
		assert.Equal(t, []byte("test-value"), value)
	case <-time.After(time.Second * 5):
		t.Fatal("did not receive message")
	}
}
