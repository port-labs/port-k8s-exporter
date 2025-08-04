package logger

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPWriter(t *testing.T) {
	// Create a test HTTP server to receive logs
	var receivedLogs []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
			return
		}
		mu.Lock()
		receivedLogs = append(receivedLogs, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Test HTTPWriter with smaller capacity for immediate flushing
	writer := NewHTTPWriter()
	writer.Capacity = 1                           // Force immediate flush for testing
	writer.FlushInterval = 100 * time.Millisecond // Shorter interval for testing

	// Configure the writer
	token, err := func() (string, error) {
		return "test-token", nil
	}()
	require.NoError(t, err)
	writer.Client = writer.Client.SetAuthScheme("Bearer").SetAuthToken(token).SetHeader("User-Agent", "port/K8S-EXPORTER/1.0.0/test")
	writer.URL = server.URL

	// Start the background goroutines
	writer.wg.Add(1)
	go writer.flushTimer()

	testLog := `{"level":"info","timestamp":"2024-01-01T00:00:00.000Z","message":"test message"}`

	n, err := writer.Write([]byte(testLog))
	require.NoError(t, err, "HTTPWriter.Write should not fail")
	require.Equal(t, len(testLog), n, "Expected %d bytes written, got %d", len(testLog), n)

	// Wait for the log to be sent
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(receivedLogs) >= 1
	}, 2*time.Second, 50*time.Millisecond, "expected at least 1 log received")

	// Properly close the writer
	err = writer.Close()
	require.NoError(t, err, "HTTPWriter.Close should not fail")

	mu.Lock()
	defer mu.Unlock()
	require.Greater(t, len(receivedLogs), 0, "should have received at least one log")

	// Parse the received log to verify structure
	var logSchema LogsSchema
	err = json.Unmarshal([]byte(receivedLogs[0]), &logSchema)
	require.NoError(t, err, "received log should be valid JSON")
	require.Greater(t, len(logSchema.Logs), 0, "should have at least one log in the schema")
	require.Equal(t, "test message", logSchema.Logs[0].Message, "log message should match")
}

func TestInitWithHTTP(t *testing.T) {
	// Clean up any existing logger
	cleanupLogger()

	// Create a test HTTP server
	var receivedLogs []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
			return
		}
		mu.Lock()
		receivedLogs = append(receivedLogs, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Initialize logger with HTTP
	err := InitWithHTTP("info", false)
	require.NoError(t, err, "InitWithHTTP should not fail")

	// Configure the HTTP writer with smaller capacity for testing
	if httpWriter != nil {
		httpWriter.Capacity = 1
		httpWriter.FlushInterval = 100 * time.Millisecond

		SetHttpWriterParametersAndStart(server.URL, func() (string, int, error) {
			return "test-token", 10, nil
		}, LoggerIntegrationData{
			IntegrationVersion:    "1.0.0",
			IntegrationIdentifier: "test",
		})
	}

	// Test that the logger works
	Infof("Test HTTP logging message")

	// Wait for the log to be received
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(receivedLogs) >= 1
	}, 2*time.Second, 50*time.Millisecond, "expected at least 1 log received")

	// Verify that at least one log was received
	mu.Lock()
	defer mu.Unlock()
	require.Greater(t, len(receivedLogs), 0, "should have received at least one log")

	// Parse the JSON log to verify it's properly formatted
	var logSchema LogsSchema
	err = json.Unmarshal([]byte(receivedLogs[0]), &logSchema)
	require.NoError(t, err, "received log should be valid JSON")
	require.Greater(t, len(logSchema.Logs), 0, "should have logs in the schema")

	found := false
	for _, logEntry := range logSchema.Logs {
		if strings.Contains(logEntry.Message, "Test HTTP logging message") {
			found = true
			break
		}
	}
	require.True(t, found, "log message should be found in received logs")

	// Clean up
	cleanupLogger()
}

func TestInitWithLevelAndHTTP(t *testing.T) {
	// Clean up any existing logger
	cleanupLogger()

	// Create a test HTTP server
	var receivedLogs []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
			return
		}
		mu.Lock()
		receivedLogs = append(receivedLogs, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Initialize logger with debug level and HTTP
	err := InitWithHTTP("debug", false)
	require.NoError(t, err, "InitWithHTTP should not fail")

	// Configure the HTTP writer with smaller capacity for testing
	if httpWriter != nil {
		httpWriter.Capacity = 1
		httpWriter.FlushInterval = 100 * time.Millisecond

		SetHttpWriterParametersAndStart(server.URL, func() (string, int, error) {
			return "test-token", 10, nil
		}, LoggerIntegrationData{
			IntegrationVersion:    "1.0.0",
			IntegrationIdentifier: "test",
		})
	}

	// Test logging at different levels
	GetLogger().Debug("Debug message")
	Infof("Info message")
	Warningf("Warning message")

	// Wait for logs to be received (info and warning should go to HTTP)
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(receivedLogs) >= 2
	}, 3*time.Second, 100*time.Millisecond, "expected at least 2 logs received")

	mu.Lock()
	defer mu.Unlock()
	// Debug logs should only go to console, info and warning should go to HTTP
	// So we should have at least 2 HTTP logs (info and warning)
	require.GreaterOrEqual(t, len(receivedLogs), 2, "expected at least 2 HTTP logs")

	// Clean up
	cleanupLogger()
}

func TestHTTPWriterFailureHandling(t *testing.T) {
	// Test with invalid URL to ensure graceful failure handling
	writer := NewHTTPWriter()
	writer.Capacity = 1 // Force immediate flush attempt

	// Configure the writer
	token, err := func() (string, error) {
		return "test-token", nil
	}()
	require.NoError(t, err)
	writer.Client = writer.Client.SetAuthScheme("Bearer").SetAuthToken(token).SetHeader("User-Agent", "port/K8S-EXPORTER/1.0.0/test")
	writer.URL = "http://invalid-url-that-should-not-exist:9999/logs"

	// Start the background goroutines
	writer.wg.Add(1)
	go writer.flushTimer()

	testLog := `{"level":"info","message":"test"}`

	// This should not fail even if the HTTP request fails
	n, err := writer.Write([]byte(testLog))
	require.NoError(t, err, "HTTPWriter.Write should not fail even with invalid URL")
	require.Equal(t, len(testLog), n, "Expected %d bytes 'written', got %d", len(testLog), n)

	// Clean up
	err = writer.Close()
	require.NoError(t, err, "HTTPWriter.Close should not fail")
}

// Helper function to properly clean up logger state
func cleanupLogger() {
	if httpWriter != nil {
		httpWriter.Close()
		httpWriter = nil
	}
	if Logger != nil {
		Logger.Sync()
		Logger = nil
	}
}
