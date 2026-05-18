package consumer

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// integrationResyncRequestTriggersResync mirrors the unmarshal + filter path in EventListener.Run.
func integrationResyncRequestTriggersResync(stateKey string, value []byte) (bool, error) {
	incomingMessage := &IntegrationResyncRequestMessage{}
	if err := json.Unmarshal(value, incomingMessage); err != nil {
		return false, err
	}
	return shouldResyncFromIntegrationResyncRequest(stateKey, incomingMessage), nil
}

func TestIntegrationResyncRequestMessage_UnmarshalTriggersResyncForMatchingStateKey(t *testing.T) {
	stateKey := "integration-1"

	// Representative payload from the integration resync requests topic (extra fields are ignored).
	payload := []byte(`{
		"context": {
			"integrationId": "integration-1",
			"orgId": "org-abc"
		},
		"requestId": "req-123"
	}`)

	triggersResync, err := integrationResyncRequestTriggersResync(stateKey, payload)
	require.NoError(t, err)
	assert.True(t, triggersResync, "matching integrationId in wire JSON should trigger resync")

	otherIntegrationPayload := []byte(`{"context":{"integrationId":"integration-2"}}`)
	triggersResync, err = integrationResyncRequestTriggersResync(stateKey, otherIntegrationPayload)
	require.NoError(t, err)
	assert.False(t, triggersResync, "non-matching integrationId should not trigger resync")

	wrongJSONKeyPayload := []byte(`{"context":{"integration_id":"integration-1"}}`)
	triggersResync, err = integrationResyncRequestTriggersResync(stateKey, wrongJSONKeyPayload)
	require.NoError(t, err)
	assert.False(t, triggersResync, "wrong JSON field name must not populate integrationId")
}
