package consumer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldResyncFromChangeLog(t *testing.T) {
	stateKey := "integration-1"

	assert.True(t, shouldResyncFromChangeLog(stateKey, &IncomingMessage{
		Diff: &struct {
			After *struct {
				Identifier string `json:"installationId"`
			} `json:"after"`
		}{
			After: &struct {
				Identifier string `json:"installationId"`
			}{
				Identifier: stateKey,
			},
		},
	}))
	assert.False(t, shouldResyncFromChangeLog(stateKey, &IncomingMessage{
		Diff: &struct {
			After *struct {
				Identifier string `json:"installationId"`
			} `json:"after"`
		}{
			After: &struct {
				Identifier string `json:"installationId"`
			}{
				Identifier: "integration-2",
			},
		},
	}))
	assert.False(t, shouldResyncFromChangeLog(stateKey, &IncomingMessage{}))
}

func TestShouldResyncFromIntegrationResyncRequest(t *testing.T) {
	stateKey := "integration-1"

	assert.True(t, shouldResyncFromIntegrationResyncRequest(stateKey, &IntegrationResyncRequestMessage{
		Context: &struct {
			IntegrationId string `json:"integrationId"`
		}{
			IntegrationId: stateKey,
		},
	}))
	assert.False(t, shouldResyncFromIntegrationResyncRequest(stateKey, &IntegrationResyncRequestMessage{
		Context: &struct {
			IntegrationId string `json:"integrationId"`
		}{
			IntegrationId: "integration-2",
		},
	}))
	assert.False(t, shouldResyncFromIntegrationResyncRequest(stateKey, &IntegrationResyncRequestMessage{}))
}
