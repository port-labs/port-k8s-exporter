package logger

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
)

func TestHTTPWriter(t *testing.T) {
	// Create a test HTTP server to receive logs
	var receivedLogs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
			return
		}
		receivedLogs = append(receivedLogs, string(body))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Test HTTPWriter
	writer := NewHTTPWriter()
	SetHttpWriterParametersAndStart(server.URL, func() (string, error) {
		return "test-token", nil
	}, LoggerIntegrationData{
		IntegrationVersion:    "1.0.0",
		IntegrationIdentifier: "test",
	})
	testLog := `{"level":"info","timestamp":"2024-01-01T00:00:00.000Z","message":"test message"}`

	n, err := writer.Write([]byte(testLog))
	if err != nil {
		t.Errorf("HTTPWriter.Write failed: %v", err)
	}

	if n != len(testLog) {
		t.Errorf("Expected %d bytes written, got %d", len(testLog), n)
	}

	// Give the HTTP request time to complete
	time.Sleep(100 * time.Millisecond)

	if len(receivedLogs) != 1 {
		t.Errorf("Expected 1 log received, got %d", len(receivedLogs))
	}

	if len(receivedLogs) > 0 && receivedLogs[0] != testLog {
		t.Errorf("Expected log '%s', got '%s'", testLog, receivedLogs[0])
	}
}

func TestInitWithHTTP(t *testing.T) {
	// Create a test HTTP server
	var receivedLogs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
			return
		}
		receivedLogs = append(receivedLogs, string(body))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Initialize logger with HTTP
	err := InitWithHTTP()
	SetHttpWriterParametersAndStart(server.URL, func() (string, error) {
		return "test-token", nil
	}, LoggerIntegrationData{
		IntegrationVersion:    "1.0.0",
		IntegrationIdentifier: "test",
	})
	if err != nil {
		t.Fatalf("InitWithHTTP failed: %v", err)
	}

	// Test that the logger works
	Infof("Test HTTP logging message")

	// Give the HTTP request time to complete
	time.Sleep(100 * time.Millisecond)

	// Verify that at least one log was received
	if len(receivedLogs) == 0 {
		t.Error("No logs received by HTTP server")
	} else {
		// Parse the JSON log to verify it's properly formatted
		var logEntry map[string]interface{}
		err := json.Unmarshal([]byte(receivedLogs[0]), &logEntry)
		if err != nil {
			t.Errorf("Received log is not valid JSON: %v", err)
		} else {
			if message, ok := logEntry["message"].(string); !ok || !strings.Contains(message, "Test HTTP logging message") {
				t.Errorf("Log message not found or incorrect: %v", logEntry)
			}
		}
	}

	// Reset the logger for other tests
	Logger = nil
}

func TestInitWithLevelAndHTTP(t *testing.T) {
	// Create a test HTTP server
	var receivedLogs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read request body: %v", err)
			return
		}
		receivedLogs = append(receivedLogs, string(body))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Initialize logger with debug level and HTTP
	err := InitWithLevelAndHTTP(zapcore.DebugLevel)
	SetHttpWriterParametersAndStart(server.URL, func() (string, error) {
		return "test-token", nil
	}, LoggerIntegrationData{
		IntegrationVersion:    "1.0.0",
		IntegrationIdentifier: "test",
	})
	if err != nil {
		t.Fatalf("InitWithLevelAndHTTP failed: %v", err)
	}

	// Test logging at different levels
	GetLogger().Debug("Debug message")
	Infof("Info message")
	Warningf("Warning message")

	// Give the HTTP requests time to complete
	time.Sleep(200 * time.Millisecond)

	// Debug logs should only go to console, info and warning should go to HTTP
	// So we should have at least 2 HTTP logs (info and warning)
	if len(receivedLogs) < 2 {
		t.Errorf("Expected at least 2 HTTP logs, got %d", len(receivedLogs))
	}

	// Reset the logger for other tests
	Logger = nil
}

func TestHTTPWriterFailureHandling(t *testing.T) {
	// Test with invalid URL to ensure graceful failure handling
	writer := NewHTTPWriter()
	SetHttpWriterParametersAndStart("http://invalid-url-that-should-not-exist:9999/logs", func() (string, error) {
		return "test-token", nil
	}, LoggerIntegrationData{
		IntegrationVersion:    "1.0.0",
		IntegrationIdentifier: "test",
	})
	testLog := `{"level":"info","message":"test"}`

	// This should not fail even if the HTTP request fails
	n, err := writer.Write([]byte(testLog))
	if err != nil {
		t.Errorf("HTTPWriter.Write should not fail even with invalid URL: %v", err)
	}

	if n != len(testLog) {
		t.Errorf("Expected %d bytes 'written', got %d", len(testLog), n)
	}
}
