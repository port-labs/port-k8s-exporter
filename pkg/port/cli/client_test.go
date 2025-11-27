package cli

import (
	"context"
	"encoding/json"
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
		{
			name:                "retries on 400 Bad Request",
			retryableStatusCode: 400,
			successAfterRetries: 2,
			expectedRetries:     2,
		},
		{
			name:                "retries on 401 Unauthorized",
			retryableStatusCode: 401,
			successAfterRetries: 1,
			expectedRetries:     1,
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

	// Create a test server that returns non-retryable status code
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle auth token endpoint
		if r.URL.Path == "/v1/auth/access_token" {
			tokenResp := port.AccessTokenResponse{
				AccessToken: "test-token",
				ExpiresIn:   3600,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(tokenResp)
			return
		}

		// Only count entity endpoint requests
		if strings.Contains(r.URL.Path, "/entities") {
			mu.Lock()
			apiRequestCount++
			mu.Unlock()
		}

		// Return 404 (not retryable)
		w.WriteHeader(404)
		w.Write([]byte(`{"ok": false, "message": "not found"}`))
	}))
	defer server.Close()

	// Create PortClient with test server URL
	config := &config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
		StateKey:         "test-state-key",
	}

	client := New(config)

	// Wait a bit for auth to complete
	time.Sleep(100 * time.Millisecond)

	// Make the actual API call
	entityReq := &port.EntityRequest{
		Identifier: "test-entity",
		Blueprint:  "test-blueprint",
		Title:      "Test Entity",
	}

	ctx := context.Background()
	_, err := client.CreateEntity(ctx, entityReq, "", false)

	// Verify the request failed
	require.Error(t, err, "request should fail on non-retryable status code")

	// Verify we did NOT retry (only 1 attempt)
	mu.Lock()
	count := apiRequestCount
	mu.Unlock()

	// Should only make 1 attempt for non-retryable status codes
	// If we get 2, it might be due to an auth refresh, but the entity endpoint should only be called once
	assert.Equal(t, 1, count, "should not retry on non-retryable status codes")
}
