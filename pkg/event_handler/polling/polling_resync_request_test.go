package polling

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type resyncPollingTestServer struct {
	t                    *testing.T
	stateKey             string
	featureFlags         []string
	integrationUpdatedAt time.Time
	resyncRequestUpdatedAt *time.Time

	mu                     sync.Mutex
	resyncRequestCallCount int
}

func newResyncPollingTestServer(t *testing.T, cfg resyncPollingTestServer) *resyncPollingTestServer {
	t.Helper()
	return &cfg
}

func (s *resyncPollingTestServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.URL.Path == "/v1/auth/access_token":
		_ = json.NewEncoder(w).Encode(port.AccessTokenResponse{
			AccessToken: "test-token",
			ExpiresIn:   3600,
		})
	case r.URL.Path == "/v1/organization":
		_ = json.NewEncoder(w).Encode(port.ResponseBody{
			OK: true,
			OrgDetails: port.OrgDetails{
				OrgId:        "test-org",
				FeatureFlags: s.featureFlags,
			},
		})
	case r.URL.Path == "/v1/integration/"+s.stateKey:
		_ = json.NewEncoder(w).Encode(port.ResponseBody{
			OK: true,
			Integration: port.Integration{
				UpdatedAt: &s.integrationUpdatedAt,
			},
		})
	case r.URL.Path == "/v1/integration/"+s.stateKey+"/resync-request":
		s.mu.Lock()
		s.resyncRequestCallCount++
		s.mu.Unlock()

		var request *port.IntegrationResyncTriggerRequest
		if s.resyncRequestUpdatedAt != nil {
			updatedAt := *s.resyncRequestUpdatedAt
			request = &port.IntegrationResyncTriggerRequest{UpdatedAt: &updatedAt}
		}
		_ = json.NewEncoder(w).Encode(struct {
			OK      bool                                 `json:"ok"`
			Request *port.IntegrationResyncTriggerRequest `json:"request"`
		}{
			OK:      true,
			Request: request,
		})
	default:
		s.t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}
}

func (s *resyncPollingTestServer) ResyncRequestCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resyncRequestCallCount
}

func newTestPollingHandler(t *testing.T, mock *resyncPollingTestServer) (*Handler, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(mock)
	t.Cleanup(server.Close)

	portClient := cli.New(&config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
		StateKey:         mock.stateKey,
	})

	return NewPollingHandler(1, mock.stateKey, portClient, nil), server
}

func TestPollIteration_ResyncRequestPolling(t *testing.T) {
	integrationUpdatedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	resyncRequestUpdatedAt := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	olderResyncRequestUpdatedAt := time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC)
	stateKey := "test-state-key"

	tests := []struct {
		name                         string
		featureFlags                 []string
		resyncRequestUpdatedAt       *time.Time
		lastResyncRequestWatermark   string
		expectResync                 bool
		expectResyncRequestFetched   bool
	}{
		{
			name:                       "feature flag disabled skips resync request polling",
			featureFlags:               []string{},
			resyncRequestUpdatedAt:     &resyncRequestUpdatedAt,
			lastResyncRequestWatermark: "",
			expectResync:               false,
			expectResyncRequestFetched: false,
		},
		{
			name: "feature flag enabled triggers resync when resync request advances",
			featureFlags: []string{
				port.OrgOceanPollingIntegrationResyncRequestsEnabledFeatureFlag,
			},
			resyncRequestUpdatedAt:     &resyncRequestUpdatedAt,
			lastResyncRequestWatermark: "",
			expectResync:               true,
			expectResyncRequestFetched: true,
		},
		{
			name: "feature flag enabled does not resync when watermark matches resync request",
			featureFlags: []string{
				port.OrgOceanPollingIntegrationResyncRequestsEnabledFeatureFlag,
			},
			resyncRequestUpdatedAt:     &resyncRequestUpdatedAt,
			lastResyncRequestWatermark: formatUpdatedAt(&resyncRequestUpdatedAt),
			expectResync:               false,
			expectResyncRequestFetched: true,
		},
		{
			name: "feature flag enabled does not resync when resync request is older than watermark",
			featureFlags: []string{
				port.OrgOceanPollingIntegrationResyncRequestsEnabledFeatureFlag,
			},
			resyncRequestUpdatedAt:     &olderResyncRequestUpdatedAt,
			lastResyncRequestWatermark: formatUpdatedAt(&resyncRequestUpdatedAt),
			expectResync:               false,
			expectResyncRequestFetched: true,
		},
		{
			name: "feature flag enabled does not resync when resync request is missing",
			featureFlags: []string{
				port.OrgOceanPollingIntegrationResyncRequestsEnabledFeatureFlag,
			},
			resyncRequestUpdatedAt:     nil,
			lastResyncRequestWatermark: "",
			expectResync:               false,
			expectResyncRequestFetched: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newResyncPollingTestServer(t, resyncPollingTestServer{
				t:                      t,
				stateKey:               stateKey,
				featureFlags:           tt.featureFlags,
				integrationUpdatedAt:   integrationUpdatedAt,
				resyncRequestUpdatedAt: tt.resyncRequestUpdatedAt,
			})

			handler, _ := newTestPollingHandler(t, mock)
			handler.lastIntegrationStateUpdatedAt = formatUpdatedAt(&integrationUpdatedAt)
			handler.lastResyncRequestUpdatedAt = tt.lastResyncRequestWatermark

			resyncCalled := false
			handler.pollIteration(func() {
				resyncCalled = true
			})

			assert.Equal(t, tt.expectResync, resyncCalled)
			assert.Equal(t, tt.expectResyncRequestFetched, mock.ResyncRequestCallCount() > 0)

			if tt.expectResync {
				assert.Equal(t, formatUpdatedAt(tt.resyncRequestUpdatedAt), handler.lastResyncRequestUpdatedAt)
			} else {
				assert.Equal(t, tt.lastResyncRequestWatermark, handler.lastResyncRequestUpdatedAt)
			}
		})
	}
}

func TestPollIteration_ResyncRequestWatermarkAdvancesAcrossIterations(t *testing.T) {
	stateKey := "test-state-key"
	integrationUpdatedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	firstResyncRequestAt := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	secondResyncRequestAt := time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC)

	mock := &resyncPollingTestServer{
		t:                    t,
		stateKey:             stateKey,
		featureFlags:         []string{port.OrgOceanPollingIntegrationResyncRequestsEnabledFeatureFlag},
		integrationUpdatedAt: integrationUpdatedAt,
		resyncRequestUpdatedAt: &firstResyncRequestAt,
	}

	handler, _ := newTestPollingHandler(t, mock)
	handler.lastIntegrationStateUpdatedAt = formatUpdatedAt(&integrationUpdatedAt)

	firstResyncCalled := false
	handler.pollIteration(func() {
		firstResyncCalled = true
	})
	require.True(t, firstResyncCalled)
	assert.Equal(t, formatUpdatedAt(&firstResyncRequestAt), handler.lastResyncRequestUpdatedAt)

	secondResyncCalled := false
	handler.pollIteration(func() {
		secondResyncCalled = true
	})
	assert.False(t, secondResyncCalled, "same resync-request timestamp should not trigger another resync")

	mock.mu.Lock()
	mock.resyncRequestUpdatedAt = &secondResyncRequestAt
	mock.mu.Unlock()

	thirdResyncCalled := false
	handler.pollIteration(func() {
		thirdResyncCalled = true
	})
	require.True(t, thirdResyncCalled, "advanced resync-request timestamp should trigger resync")
	assert.Equal(t, formatUpdatedAt(&secondResyncRequestAt), handler.lastResyncRequestUpdatedAt)
}

func TestIsResyncRequestsPollingEnabled_CachesOnlyOnSuccess(t *testing.T) {
	stateKey := "test-state-key"
	orgRequestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/v1/auth/access_token":
			_ = json.NewEncoder(w).Encode(port.AccessTokenResponse{
				AccessToken: "test-token",
				ExpiresIn:   3600,
			})
		case r.URL.Path == "/v1/organization":
			orgRequestCount++
			if orgRequestCount == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"ok": false}`))
				return
			}
			_ = json.NewEncoder(w).Encode(port.ResponseBody{
				OK: true,
				OrgDetails: port.OrgDetails{
					OrgId: "test-org",
					FeatureFlags: []string{
						port.OrgOceanPollingIntegrationResyncRequestsEnabledFeatureFlag,
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	portClient := cli.New(&config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
		StateKey:         stateKey,
	})
	handler := NewPollingHandler(1, stateKey, portClient, nil)

	assert.False(t, handler.isResyncRequestsPollingEnabled())
	assert.Nil(t, handler.resyncRequestsPollingEnabled)

	assert.True(t, handler.isResyncRequestsPollingEnabled())
	require.NotNil(t, handler.resyncRequestsPollingEnabled)
	assert.True(t, *handler.resyncRequestsPollingEnabled)
	assert.Equal(t, 2, orgRequestCount)
}

func TestPollIteration_IntegrationChangeTakesPrecedenceOverResyncRequest(t *testing.T) {
	stateKey := "test-state-key"
	initialIntegrationAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	updatedIntegrationAt := time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)
	resyncRequestAt := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

	mock := &resyncPollingTestServer{
		t:                      t,
		stateKey:               stateKey,
		featureFlags:           []string{port.OrgOceanPollingIntegrationResyncRequestsEnabledFeatureFlag},
		integrationUpdatedAt:   updatedIntegrationAt,
		resyncRequestUpdatedAt: &resyncRequestAt,
	}

	handler, _ := newTestPollingHandler(t, mock)
	handler.lastIntegrationStateUpdatedAt = formatUpdatedAt(&initialIntegrationAt)

	resyncCalled := false
	handler.pollIteration(func() {
		resyncCalled = true
	})

	require.True(t, resyncCalled)
	assert.Equal(t, formatUpdatedAt(&updatedIntegrationAt), handler.lastIntegrationStateUpdatedAt)
	assert.Empty(t, handler.lastResyncRequestUpdatedAt)
	assert.Equal(t, 0, mock.ResyncRequestCallCount())
}

func TestPollIteration_ResyncRequestPathUsesPollingQueryParam(t *testing.T) {
	stateKey := "test-state-key"
	integrationUpdatedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var integrationQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/v1/auth/access_token":
			_ = json.NewEncoder(w).Encode(port.AccessTokenResponse{
				AccessToken: "test-token",
				ExpiresIn:   3600,
			})
		case r.URL.Path == "/v1/organization":
			_ = json.NewEncoder(w).Encode(port.ResponseBody{
				OK: true,
				OrgDetails: port.OrgDetails{
					OrgId:        "test-org",
					FeatureFlags: []string{port.OrgOceanPollingIntegrationResyncRequestsEnabledFeatureFlag},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/v1/integration/"):
			if strings.HasSuffix(r.URL.Path, "/resync-request") {
				updatedAt := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
				_ = json.NewEncoder(w).Encode(struct {
					OK      bool                                 `json:"ok"`
					Request *port.IntegrationResyncTriggerRequest `json:"request"`
				}{
					OK:      true,
					Request: &port.IntegrationResyncTriggerRequest{UpdatedAt: &updatedAt},
				})
				return
			}
			integrationQuery = r.URL.Query().Get("isPolling")
			_ = json.NewEncoder(w).Encode(port.ResponseBody{
				OK: true,
				Integration: port.Integration{
					UpdatedAt: &integrationUpdatedAt,
				},
			})
		default:
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	portClient := cli.New(&config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
		StateKey:         stateKey,
	})
	handler := NewPollingHandler(1, stateKey, portClient, nil)
	handler.lastIntegrationStateUpdatedAt = formatUpdatedAt(&integrationUpdatedAt)

	handler.pollIteration(func() {})

	assert.Equal(t, "true", integrationQuery)
}
