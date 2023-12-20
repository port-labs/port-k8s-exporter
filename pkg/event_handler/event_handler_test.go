package event_handler

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

type fixture struct {
	t *testing.T
}

type ControllerHandlerMock struct {
	stopped bool
}

func (c *ControllerHandlerMock) Stop() {
	c.stopped = true
}

type EventListenerMock struct {
}

func (e *EventListenerMock) Run(resync func()) error {
	resync()
	resync()
	return nil
}

func TestStartKafkaEventListener(t *testing.T) {
	eventListenerMock := &EventListenerMock{}
	firstResponse := &ControllerHandlerMock{}
	secondResponse := &ControllerHandlerMock{}
	thirdResponse := &ControllerHandlerMock{}
	responses := []*ControllerHandlerMock{
		firstResponse,
		secondResponse,
		thirdResponse,
	}

	err := StartEventHandler(eventListenerMock, func() (IStoppableRsync, error) {
		r := responses[0]
		responses = responses[1:]

		return r, nil
	})

	if err != nil {
		t.Errorf("Expected no error, got %s", err.Error())
	}

	assert.True(t, firstResponse.stopped)
	assert.True(t, secondResponse.stopped)
	assert.False(t, thirdResponse.stopped)
}
