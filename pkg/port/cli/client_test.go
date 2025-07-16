package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
)

// TestAuthenticate_RemovesAuthorizationHeader ensures that the Authenticate call does NOT
// send an Authorization header – regardless of whether the underlying Resty client
// already has one configured via SetAuthToken.
func TestAuthenticate_RemovesAuthorizationHeader(t *testing.T) {
	// Arrange an HTTP test server that records incoming requests.
	var sawAuthHeader bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawAuthHeader = true
		}
		// Respond with a minimal valid payload.
		_ = json.NewEncoder(w).Encode(map[string]string{"accessToken": "new-token"})
	}))
	defer srv.Close()

	// Build a PortClient pointing at the test server.
	appCfg := &config.ApplicationConfiguration{
		PortBaseURL:      srv.URL + "/", // Resty joins base URL + relative path.
		PortClientId:     "test-id",
		PortClientSecret: "test-secret",
	}

	pc := New(appCfg)

	// Simulate a previously set token that would normally add an Authorization header.
	pc.Client.SetAuthToken("old-token")

	// Act – perform authentication.
	_, err := pc.Authenticate(context.Background(), pc.ClientID, pc.ClientSecret)
	if err != nil {
		t.Fatalf("Authenticate returned error: %v", err)
	}

	// Assert – the server must not have observed an Authorization header.
	if sawAuthHeader {
		t.Fatalf("expected no Authorization header to be sent, but one was observed")
	}
}
