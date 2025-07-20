package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

func TestAuthenticate_Success(t *testing.T) {
	// Mock server that returns a valid token
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/access_token" {
			t.Errorf("Expected path /v1/auth/access_token, got %s", r.URL.Path)
		}

		response := port.AccessTokenResponse{
			Ok:          true,
			AccessToken: "test-token-123",
			ExpiresIn:   3600,
			TokenType:   "Bearer",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client
	config := &config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
	}
	client := New(config)

	// Test authentication
	token, err := client.Authenticate(context.Background(), "test-client-id", "test-client-secret")
	if err != nil {
		t.Fatalf("Authentication failed: %v", err)
	}

	if token != "test-token-123" {
		t.Errorf("Expected token 'test-token-123', got '%s'", token)
	}

	// Verify token is set on the client
	if client.Client.Token != "test-token-123" {
		t.Errorf("Expected client token 'test-token-123', got '%s'", client.Client.Token)
	}
}

func TestAuthenticate_InvalidCredentials(t *testing.T) {
	// Mock server that returns 401
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Invalid credentials"}`))
	}))
	defer server.Close()

	config := &config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "invalid-id",
		PortClientSecret: "invalid-secret",
	}
	client := New(config)

	// Test authentication failure
	_, err := client.Authenticate(context.Background(), "invalid-id", "invalid-secret")
	if err == nil {
		t.Fatal("Expected authentication to fail, but it succeeded")
	}

	// Verify error message contains useful info
	expectedErr := "failed to authenticate"
	if err.Error()[:len(expectedErr)] != expectedErr {
		t.Errorf("Expected error to start with '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestAuthenticate_Concurrent(t *testing.T) {
	// This test checks for race conditions when multiple goroutines authenticate simultaneously
	authCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCount++
		// Add a small delay to increase chance of race conditions
		time.Sleep(10 * time.Millisecond)

		response := port.AccessTokenResponse{
			Ok:          true,
			AccessToken: "concurrent-token-123",
			ExpiresIn:   3600,
			TokenType:   "Bearer",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	config := &config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
	}
	client := New(config)

	// Launch multiple concurrent authentication requests
	numGoroutines := 10
	var wg sync.WaitGroup
	errors := make([]error, numGoroutines)
	tokens := make([]string, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			token, err := client.Authenticate(context.Background(), "test-client-id", "test-client-secret")
			errors[index] = err
			tokens[index] = token
		}(i)
	}

	wg.Wait()

	// Check results
	successCount := 0
	for i := 0; i < numGoroutines; i++ {
		if errors[i] == nil {
			successCount++
			if tokens[i] != "concurrent-token-123" {
				t.Errorf("Goroutine %d: expected token 'concurrent-token-123', got '%s'", i, tokens[i])
			}
		} else {
			t.Logf("Goroutine %d failed: %v", i, errors[i])
		}
	}

	if successCount == 0 {
		t.Fatal("All authentication attempts failed")
	}

	t.Logf("Concurrent test: %d/%d goroutines succeeded, %d auth requests made", successCount, numGoroutines, authCount)

	// Final token should be set correctly
	if client.Client.Token != "concurrent-token-123" {
		t.Errorf("Final client token should be 'concurrent-token-123', got '%s'", client.Client.Token)
	}
}

func TestAuthenticate_ClearAndRestore(t *testing.T) {
	// Test that demonstrates the "no token supplied" issue
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := port.AccessTokenResponse{
			Ok:          true,
			AccessToken: "new-token-456",
			ExpiresIn:   3600,
			TokenType:   "Bearer",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	config := &config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
	}
	client := New(config)

	// Set an initial token
	client.Client.SetAuthToken("initial-token-123")

	// Verify initial token is set
	if client.Client.Token != "initial-token-123" {
		t.Fatalf("Initial token not set correctly")
	}

	// Authenticate (this will clear and then set a new token)
	token, err := client.Authenticate(context.Background(), "test-client-id", "test-client-secret")
	if err != nil {
		t.Fatalf("Authentication failed: %v", err)
	}

	if token != "new-token-456" {
		t.Errorf("Expected new token 'new-token-456', got '%s'", token)
	}

	// Verify new token is set
	if client.Client.Token != "new-token-456" {
		t.Errorf("Expected client token 'new-token-456', got '%s'", client.Client.Token)
	}
}

// Benchmark test for authentication performance
func BenchmarkAuthenticate(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := port.AccessTokenResponse{
			Ok:          true,
			AccessToken: "bench-token-123",
			ExpiresIn:   3600,
			TokenType:   "Bearer",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	config := &config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
	}
	client := New(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.Authenticate(context.Background(), "test-client-id", "test-client-secret")
		if err != nil {
			b.Fatalf("Authentication failed: %v", err)
		}
	}
}

// --- Race Condition and Stress Tests ---
// (All race condition tests and AuthorizationError are defined only once below)

// TestRaceCondition_AuthenticateAndAPICall demonstrates the race condition
// that can occur when one goroutine is authenticating while another is making an API call
func TestRaceCondition_AuthenticateAndAPICall(t *testing.T) {
	authCallCount := 0
	apiCallCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/access_token":
			authCallCount++
			// Simulate slow authentication
			time.Sleep(50 * time.Millisecond)
			response := port.AccessTokenResponse{
				Ok:          true,
				AccessToken: "race-test-token",
				ExpiresIn:   3600,
				TokenType:   "Bearer",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case "/v1/blueprints/test/entities":
			apiCallCount++
			// Check if we have an authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				t.Logf("API call made without authorization header!")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error": "no token supplied to authorization header"}`))
				return
			}

			// Simulate API response
			response := port.ResponseBody{
				OK: true,
				Entity: port.Entity{
					Identifier: "test-entity",
					Blueprint:  "test",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	config := &config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
	}
	client := New(config)

	// Set an initial token
	client.Client.SetAuthToken("initial-token")

	numIterations := 20
	var wg sync.WaitGroup
	errors := make([]error, numIterations*2) // *2 because we have two types of operations

	// Launch concurrent operations
	for i := 0; i < numIterations; i++ {
		// Authentication goroutine
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := client.Authenticate(context.Background(), "test-client-id", "test-client-secret")
			if err != nil {
				errors[index*2] = err
			}
		}(i)

		// API call goroutine (simulating BulkUpsertEntities or PostIntegrationKindExample)
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			// Small delay to increase chance of race condition
			time.Sleep(25 * time.Millisecond)

			resp, err := client.Client.R().
				SetBody(port.EntityRequest{
					Identifier: "test-entity",
					Blueprint:  "test",
				}).
				Post("v1/blueprints/test/entities")

			if err != nil {
				errors[index*2+1] = err
			} else if resp.StatusCode() == 401 {
				errors[index*2+1] = &AuthorizationError{Message: "no token supplied to authorization header"}
			}
		}(i)

		// Small delay between iterations
		time.Sleep(10 * time.Millisecond)
	}

	wg.Wait()

	// Analyze results
	authErrors := 0
	apiErrors := 0
	authorizationErrors := 0

	for i := 0; i < len(errors); i += 2 {
		if errors[i] != nil {
			authErrors++
			t.Logf("Auth error %d: %v", i/2, errors[i])
		}
		if errors[i+1] != nil {
			apiErrors++
			if _, ok := errors[i+1].(*AuthorizationError); ok {
				authorizationErrors++
			}
			t.Logf("API error %d: %v", i/2, errors[i+1])
		}
	}

	t.Logf("Test results:")
	t.Logf("  Auth calls: %d", authCallCount)
	t.Logf("  API calls: %d", apiCallCount)
	t.Logf("  Auth errors: %d/%d", authErrors, numIterations)
	t.Logf("  API errors: %d/%d", apiErrors, numIterations)
	t.Logf("  Authorization errors: %d", authorizationErrors)

	// If we see authorization errors, it indicates the race condition occurred
	if authorizationErrors > 0 {
		t.Logf("⚠️  Race condition detected: %d authorization errors occurred", authorizationErrors)
		t.Logf("This demonstrates the 'no token supplied to authorization header' issue")
	} else {
		t.Logf("✅ No race condition detected in this run")
	}
}

func TestRaceCondition_PostIntegrationKindExample(t *testing.T) {
	const (
		stateKey = "race-state"
		kind     = "race-kind"
	)
	authCallCount := 0
	exampleCallCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/access_token":
			authCallCount++
			// Simulate slow authentication
			time.Sleep(50 * time.Millisecond)
			response := port.AccessTokenResponse{
				Ok:          true,
				AccessToken: "race-test-token",
				ExpiresIn:   3600,
				TokenType:   "Bearer",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		case r.URL.Path == "/v1/integration/"+stateKey+"/kinds/"+kind+"/examples":
			exampleCallCount++
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				t.Logf("Example call made without authorization header!")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error": "no token supplied to authorization header"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok": true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	config := &config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
	}
	client := New(config)

	// Set an initial token
	client.Client.SetAuthToken("initial-token")

	numIterations := 20
	var wg sync.WaitGroup
	errors := make([]error, numIterations*2)

	for i := 0; i < numIterations; i++ {
		// Authentication goroutine
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := client.Authenticate(context.Background(), "test-client-id", "test-client-secret")
			if err != nil {
				errors[index*2] = err
			}
		}(i)

		// PostIntegrationKindExample goroutine
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			// Small delay to increase chance of race condition
			time.Sleep(25 * time.Millisecond)
			err := client.PostIntegrationKindExample(stateKey, kind, []interface{}{map[string]interface{}{"foo": "bar"}})
			if err != nil {
				errors[index*2+1] = err
			}
		}(i)

		// Small delay between iterations
		time.Sleep(10 * time.Millisecond)
	}

	wg.Wait()

	exampleAuthErrors := 0
	for i := 1; i < len(errors); i += 2 {
		if errors[i] != nil && errors[i].Error() == "failed to post integration kind example, got: {\"error\": \"no token supplied to authorization header\"}" {
			exampleAuthErrors++
			t.Logf("Example API error %d: %v", i/2, errors[i])
		}
	}

	t.Logf("Test results:")
	t.Logf("  Auth calls: %d", authCallCount)
	t.Logf("  Example calls: %d", exampleCallCount)
	t.Logf("  Example API authorization errors: %d/%d", exampleAuthErrors, numIterations)

	if exampleAuthErrors > 0 {
		t.Logf("⚠️  Race condition detected: %d example API calls without token", exampleAuthErrors)
	} else {
		t.Logf("✅ No race condition detected in this run")
	}
}

// AuthorizationError represents the specific error we're looking for
// (Only define if not already present)
type AuthorizationError struct {
	Message string
}

func (e *AuthorizationError) Error() string {
	return e.Message
}

// TestRaceCondition_StressTest puts more pressure on the system to expose race conditions
func TestRaceCondition_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/access_token":
			// Very short delay
			time.Sleep(5 * time.Millisecond)
			response := port.AccessTokenResponse{
				Ok:          true,
				AccessToken: "stress-token",
				ExpiresIn:   3600,
				TokenType:   "Bearer",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)

		case "/v1/blueprints/test/entities/bulk":
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error": "no token supplied to authorization header"}`))
				return
			}

			response := port.BulkUpsertResponse{
				OK:       true,
				Entities: []port.BulkEntityResult{},
				Errors:   []port.BulkEntityError{},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer server.Close()

	config := &config.ApplicationConfiguration{
		PortBaseURL:      server.URL,
		PortClientId:     "test-client-id",
		PortClientSecret: "test-client-secret",
	}
	client := New(config)

	numGoroutines := 50
	duration := 5 * time.Second
	stop := make(chan struct{})

	var wg sync.WaitGroup
	var authErrors, apiErrors, authorizationErrors int64
	var mu sync.Mutex

	// Start authentication workers
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, err := client.Authenticate(context.Background(), "test-client-id", "test-client-secret")
					if err != nil {
						mu.Lock()
						authErrors++
						mu.Unlock()
					}
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// Start API call workers
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					resp, err := client.BulkUpsertEntities(
						context.Background(),
						"test",
						[]port.EntityRequest{},
						"test-run",
						false,
					)
					if err != nil {
						mu.Lock()
						apiErrors++
						if err.Error() == "failed to bulk upsert entities, got status 401: {\"error\": \"no token supplied to authorization header\"}" {
							authorizationErrors++
						}
						mu.Unlock()
					} else if resp != nil && !resp.OK {
						mu.Lock()
						apiErrors++
						mu.Unlock()
					}
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// Run for specified duration
	time.Sleep(duration)
	close(stop)
	wg.Wait()

	t.Logf("Stress test results over %v:", duration)
	t.Logf("  Auth errors: %d", authErrors)
	t.Logf("  API errors: %d", apiErrors)
	t.Logf("  Authorization errors: %d", authorizationErrors)

	if authorizationErrors > 0 {
		t.Logf("⚠️  Race condition detected: %d authorization errors occurred", authorizationErrors)
	} else {
		t.Logf("✅ No race condition detected in this stress test")
	}
}
