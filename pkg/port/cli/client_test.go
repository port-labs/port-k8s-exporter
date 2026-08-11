package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortClient_RetriesOnRetryableStatusCodes(t *testing.T) {
	tests := []struct {
		name                string
		retryableStatusCode int
		successAfterRetries int
		expectedRetries     int
	}{
		{
			name:                "retries on 429 Too Many Requests",
			retryableStatusCode: 429,
			successAfterRetries: 2,
			expectedRetries:     2,
		},
		{
			name:                "retries on 502 Bad Gateway",
			retryableStatusCode: 502,
			successAfterRetries: 3,
			expectedRetries:     3,
		},
		{
			name:                "retries on 503 Service Unavailable",
			retryableStatusCode: 503,
			successAfterRetries: 1,
			expectedRetries:     1,
		},
		{
			name:                "retries on 504 Gateway Timeout",
			retryableStatusCode: 504,
			successAfterRetries: 2,
			expectedRetries:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var apiRequestCount int
			var mu sync.Mutex

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/auth/access_token" {
					tokenResp := port.AccessTokenResponse{
						AccessToken: "test-token",
						ExpiresIn:   3600,
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(tokenResp)
					return
				}

				if strings.Contains(r.URL.Path, "/entities") {
					mu.Lock()
					apiRequestCount++
					currentCount := apiRequestCount
					mu.Unlock()

					if currentCount <= tt.successAfterRetries {
						w.WriteHeader(tt.retryableStatusCode)
						w.Write([]byte(`{"ok": false, "message": "retryable error"}`))
					} else {
						response := port.ResponseBody{
							OK: true,
							Entity: port.Entity{
								Identifier: "test-entity",
								Blueprint:  "test-blueprint",
							},
						}
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(response)
					}
					return
				}

				w.WriteHeader(404)
			}))
			defer server.Close()

			config := &config.ApplicationConfiguration{
				PortBaseURL:      server.URL,
				PortClientId:     "test-client-id",
				PortClientSecret: "test-client-secret",
				StateKey:         "test-state-key",
			}

			client := New(config)

			time.Sleep(100 * time.Millisecond)

			entityReq := &port.EntityRequest{
				Identifier: "test-entity",
				Blueprint:  "test-blueprint",
				Title:      "Test Entity",
			}

			ctx := context.Background()
			_, err := client.CreateEntity(ctx, entityReq, "", false)

			require.NoError(t, err, "request should eventually succeed after retries")

			mu.Lock()
			count := apiRequestCount
			mu.Unlock()

			expectedTotalRequests := tt.successAfterRetries + 1
			assert.Equal(t, expectedTotalRequests, count,
				"should have retried %d times before success", tt.expectedRetries)
		})
	}
}

func TestPortClient_StopsRetryingAfterMaxRetries(t *testing.T) {
	var apiRequestCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/access_token" {
			tokenResp := port.AccessTokenResponse{
				AccessToken: "test-token",
				ExpiresIn:   3600,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tokenResp)
			return
		}

		if strings.Contains(r.URL.Path, "/entities") {
			mu.Lock()
			apiRequestCount++
			mu.Unlock()
		}

		w.WriteHeader(429)
		w.Write([]byte(`{"ok": false, "message": "rate limited"}`))
	}))
	defer server.Close()

	config := &config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
		StateKey:         "test-state-key",
	}

	client := New(config)

	time.Sleep(100 * time.Millisecond)

	entityReq := &port.EntityRequest{
		Identifier: "test-entity",
		Blueprint:  "test-blueprint",
		Title:      "Test Entity",
	}

	ctx := context.Background()
	_, err := client.CreateEntity(ctx, entityReq, "", false)

	require.Error(t, err, "request should fail after max retries")

	mu.Lock()
	count := apiRequestCount
	mu.Unlock()

	// With SetRetryCount(5), resty may retry up to 5 times after the initial request
	// The actual count may be 6 (1 initial + 5 retries) or 7 depending on resty's internal behavior
	// We check that it's at least 6 and at most 7
	assert.GreaterOrEqual(t, count, 6, "should have attempted at least 6 times (1 initial + 5 retries)")
	assert.LessOrEqual(t, count, 7, "should not exceed 7 attempts")
}

func TestPortClient_DoesNotRetryOnNonRetryableStatusCodes(t *testing.T) {
	var apiRequestCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/access_token" {
			tokenResp := port.AccessTokenResponse{
				AccessToken: "test-token",
				ExpiresIn:   3600,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tokenResp)
			return
		}

		if strings.Contains(r.URL.Path, "/entities") {
			mu.Lock()
			apiRequestCount++
			mu.Unlock()
		}

		w.WriteHeader(404)
		w.Write([]byte(`{"ok": false, "message": "not found"}`))
	}))
	defer server.Close()

	config := &config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
		StateKey:         "test-state-key",
	}

	client := New(config)

	time.Sleep(100 * time.Millisecond)

	entityReq := &port.EntityRequest{
		Identifier: "test-entity",
		Blueprint:  "test-blueprint",
		Title:      "Test Entity",
	}

	ctx := context.Background()
	_, err := client.CreateEntity(ctx, entityReq, "", false)

	require.Error(t, err, "request should fail on non-retryable status code")

	mu.Lock()
	count := apiRequestCount
	mu.Unlock()

	// Should only make 1 attempt for non-retryable status codes
	// If we get 2, it might be due to an auth refresh, but the entity endpoint should only be called once
	assert.Equal(t, 1, count, "should not retry on non-retryable status codes")
}

func TestIsBulkNonRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"401 is non-retryable", &BulkUpsertError{StatusCode: 401, Body: "unauthorized"}, true},
		{"403 is non-retryable", &BulkUpsertError{StatusCode: 403, Body: "forbidden"}, true},
		{"404 is non-retryable", &BulkUpsertError{StatusCode: 404, Body: "not found"}, true},
		{"422 is non-retryable", &BulkUpsertError{StatusCode: 422, Body: "validation error"}, true},
		{"500 is retryable", &BulkUpsertError{StatusCode: 500, Body: "internal error"}, false},
		{"502 is retryable", &BulkUpsertError{StatusCode: 502, Body: "bad gateway"}, false},
		{"400 is retryable", &BulkUpsertError{StatusCode: 400, Body: "bad request"}, false},
		{"plain error is retryable", fmt.Errorf("network timeout"), false},
		{"nil error is retryable", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				assert.False(t, IsBulkNonRetryableError(tt.err))
			} else {
				assert.Equal(t, tt.expected, IsBulkNonRetryableError(tt.err))
			}
		})
	}
}

func newBulkTestServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/access_token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(port.AccessTokenResponse{
				AccessToken: "test-token",
				ExpiresIn:   3600,
			})
			return
		}
		handler(w, r)
	}))
}

func newTestClient(serverURL string) *PortClient {
	cfg := &config.ApplicationConfiguration{
		PortBaseURL:      serverURL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
		StateKey:         "test-state-key",
	}
	client := New(cfg)
	time.Sleep(100 * time.Millisecond)
	return client
}

func TestBulkUpsert_ReturnsTypedErrorWithStatusCode(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		nonRetryable   bool
	}{
		{"404 returns non-retryable BulkUpsertError", 404, true},
		{"422 returns non-retryable BulkUpsertError", 422, true},
		{"500 returns retryable BulkUpsertError", 500, false},
		{"502 returns retryable BulkUpsertError", 502, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newBulkTestServer(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(`{"ok": false, "message": "error"}`))
			})
			defer server.Close()

			client := newTestClient(server.URL)
			entities := []port.EntityRequest{{Identifier: "e1", Blueprint: "test-bp"}}

			_, err := client.BulkUpsertEntities(context.Background(), "test-bp", entities, "", false)

			require.Error(t, err)
			var bulkErr *BulkUpsertError
			require.ErrorAs(t, err, &bulkErr)
			assert.Equal(t, tt.statusCode, bulkErr.StatusCode)
			assert.Equal(t, tt.nonRetryable, IsBulkNonRetryableError(err))
		})
	}
}

func TestBulkUpsert_SuccessReturnsNoError(t *testing.T) {
	server := newBulkTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(port.BulkUpsertResponse{
			OK: true,
			Entities: []port.BulkEntityResult{
				{Identifier: "e1", Created: true},
				{Identifier: "e2", Created: true},
			},
			Errors: []port.BulkEntityError{},
		})
	})
	defer server.Close()

	client := newTestClient(server.URL)
	entities := []port.EntityRequest{
		{Identifier: "e1", Blueprint: "test-bp"},
		{Identifier: "e2", Blueprint: "test-bp"},
	}

	resp, err := client.BulkUpsertEntities(context.Background(), "test-bp", entities, "", false)

	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Len(t, resp.Entities, 2)
	assert.Empty(t, resp.Errors)
}

func TestBulkUpsert_PartialSuccess207(t *testing.T) {
	server := newBulkTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(207)
		json.NewEncoder(w).Encode(port.BulkUpsertResponse{
			OK: true,
			Entities: []port.BulkEntityResult{
				{Identifier: "e1", Created: true},
			},
			Errors: []port.BulkEntityError{
				{Identifier: "e2", StatusCode: 422, Message: "validation failed"},
				{Identifier: "e3", StatusCode: 500, Message: "internal error"},
			},
		})
	})
	defer server.Close()

	client := newTestClient(server.URL)
	entities := []port.EntityRequest{
		{Identifier: "e1", Blueprint: "test-bp"},
		{Identifier: "e2", Blueprint: "test-bp"},
		{Identifier: "e3", Blueprint: "test-bp"},
	}

	resp, err := client.BulkUpsertEntities(context.Background(), "test-bp", entities, "", false)

	require.NoError(t, err)
	assert.Len(t, resp.Entities, 1)
	assert.Len(t, resp.Errors, 2)

	for _, bulkError := range resp.Errors {
		if bulkError.Identifier == "e2" {
			assert.Equal(t, 422, bulkError.StatusCode, "e2 should have 422 (non-retryable)")
		}
		if bulkError.Identifier == "e3" {
			assert.Equal(t, 500, bulkError.StatusCode, "e3 should have 500 (retryable)")
		}
	}
}

func TestBulkUpsert_NetworkErrorIsRetryable(t *testing.T) {
	server := newBulkTestServer(func(w http.ResponseWriter, r *http.Request) {})
	server.Close()

	client := newTestClient(server.URL)
	entities := []port.EntityRequest{{Identifier: "e1", Blueprint: "test-bp"}}

	_, err := client.BulkUpsertEntities(context.Background(), "test-bp", entities, "", false)

	require.Error(t, err)
	assert.False(t, IsBulkNonRetryableError(err), "network errors should be retryable")
}
