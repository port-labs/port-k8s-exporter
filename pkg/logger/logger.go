package logger

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.SugaredLogger
var httpWriter *HTTPWriter // Keep reference for cleanup

// LogRecord represents a serialized log record
type LogRecord struct {
	Message   string                 `json:"message"`
	Level     string                 `json:"level"`
	Timestamp string                 `json:"timestamp"`
	Extra     map[string]interface{} `json:"extra,omitempty"`
}

type LogsSchema struct {
	Logs []LogRecord `json:"logs"`
}

type LoggerIntegrationData struct {
	IntegrationVersion    string
	IntegrationIdentifier string
}

// HTTPWriter implements io.Writer to send logs to HTTP endpoint with buffering
type HTTPWriter struct {
	URL           string
	Client        *resty.Client
	Capacity      int           // Maximum number of log records to buffer
	FlushInterval time.Duration // Time interval to flush logs

	mu            sync.Mutex
	buffer        []LogRecord
	lastFlushTime time.Time
	done          chan struct{}
	wg            sync.WaitGroup
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewHTTPWriter creates a new HTTP writer with buffering
func NewHTTPWriter() *HTTPWriter {
	ctx, cancel := context.WithCancel(context.Background())
	w := &HTTPWriter{
		URL:           "",
		Client:        resty.New().SetTimeout(10 * time.Second),
		Capacity:      100,
		FlushInterval: 5 * time.Second,
		buffer:        make([]LogRecord, 0),
		lastFlushTime: time.Now(),
		done:          make(chan struct{}),
		ctx:           ctx,
		cancel:        cancel,
	}

	return w
}

func convertLogLevelToPortLogLevel(level string) (string, error) {
	level = strings.ToUpper(level)
	switch level {
	case "DEBUG":
		return "debug", nil
	case "INFO":
		return "info", nil
	case "WARN":
		return "warning", nil
	case "ERROR":
		return "error", nil
	case "FATAL", "PANIC":
		return "fatal", nil
	}
	return "", errors.New("invalid log level: " + level)
}

// Write implements io.Writer interface with buffering
func (w *HTTPWriter) Write(p []byte) (n int, err error) {
	// Parse the log entry to extract structured data
	var logData map[string]interface{}
	if err := json.Unmarshal(p, &logData); err != nil {
		// If parsing fails, treat as plain text message
		logData = map[string]interface{}{
			"message":   string(p),
			"level":     "INFO",
			"timestamp": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		}
	}
	logLevel, err := convertLogLevelToPortLogLevel(getString(logData, "level"))
	if err != nil {
		//we don't know the log level, so we default to info
		logLevel = "info"
	}
	// Create log record
	record := LogRecord{
		Message:   getString(logData, "message"),
		Level:     logLevel,
		Timestamp: getString(logData, "timestamp"),
		Extra:     getExtra(logData),
	}

	// If timestamp is missing, add it
	if record.Timestamp == "" {
		record.Timestamp = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}

	w.mu.Lock()
	w.buffer = append(w.buffer, record)
	shouldFlush := w.shouldFlush()
	w.mu.Unlock()

	if shouldFlush {
		w.flush()
	}

	return len(p), nil
}

// shouldFlush determines if the buffer should be flushed
func (w *HTTPWriter) shouldFlush() bool {
	if w.URL == "" {
		return false
	}

	bufferLen := len(w.buffer)
	if bufferLen == 0 {
		return false
	}

	// Check capacity
	if bufferLen >= w.Capacity {
		return true
	}

	return false
}

// flush sends buffered logs asynchronously
func (w *HTTPWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buffer) == 0 {
		return
	}

	// Copy buffer and clear it
	logs := make([]LogRecord, len(w.buffer))
	copy(logs, w.buffer)
	w.buffer = w.buffer[:0] // Clear buffer but keep capacity
	w.lastFlushTime = time.Now()

	// Send logs asynchronously
	w.wg.Add(1)
	go w.sendLogs(logs)
}

// sendLogs sends logs to the HTTP endpoint
func (w *HTTPWriter) sendLogs(logs []LogRecord) {
	defer w.wg.Done()

	_, err := w.Client.R().SetBody(LogsSchema{Logs: logs}).Post(w.URL)
	if err != nil {
		// Don't fail logging if HTTP request fails
		// This prevents logging from breaking if the HTTP endpoint is down
		return
	}
}

// flushTimer runs a background timer to flush logs periodically
func (w *HTTPWriter) flushTimer() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if time.Since(w.lastFlushTime) >= w.FlushInterval {
				w.flush()
			}
		case <-w.done:
			return
		case <-w.ctx.Done():
			return
		}
	}
}

// Close gracefully shuts down the HTTP writer
func (w *HTTPWriter) Close() error {
	w.cancel()

	// Only close the channel if it's still open
	select {
	case <-w.done:
		// Already closed
	default:
		close(w.done)
	}

	// Flush any remaining logs
	w.flush()

	// Wait for all goroutines to finish
	w.wg.Wait()

	return nil
}

// Helper functions
func getString(data map[string]interface{}, key string) string {
	if val, ok := data[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getExtra(data map[string]interface{}) map[string]interface{} {
	extra := make(map[string]interface{})
	exclude := map[string]bool{
		"message":   true,
		"level":     true,
		"timestamp": true,
	}

	for k, v := range data {
		if !exclude[k] {
			extra[k] = v
		}
	}

	if len(extra) == 0 {
		return nil
	}
	return extra
}

func SetHttpWriterParametersAndStart(url string, authFunc func() (string, int, error), integrationData LoggerIntegrationData) {
	if httpWriter == nil {
		return
	}
	token, _, err := authFunc()
	if err != nil {
		return
	}
	httpWriter.Client = httpWriter.Client.SetAuthScheme("Bearer").SetAuthToken(token).SetHeader("User-Agent", "port/K8S-EXPORTER/"+integrationData.IntegrationVersion+"/"+integrationData.IntegrationIdentifier).OnBeforeRequest(func(c *resty.Client, r *resty.Request) error {
		token, _, err := authFunc()
		if err != nil {
			return err
		}
		httpWriter.Client.SetAuthToken(token)
		return nil
	})
	httpWriter.URL = url
	httpWriter.wg.Add(1)
	go httpWriter.flushTimer()
}

// InitWithLevel initializes the global zap logger with a specific log level (console only)
func Init(level string, debugMode bool) error {
	config := zap.NewProductionConfig()
	parsedLevel, err := zap.ParseAtomicLevel(level)
	if err != nil {
		parsedLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	}
	if debugMode {
		parsedLevel = zap.NewAtomicLevelAt(zap.DebugLevel)
	}
	config.Level = parsedLevel
	config.OutputPaths = []string{"stdout"}
	config.ErrorOutputPaths = []string{"stderr"}
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.MessageKey = "message"
	config.EncoderConfig.LevelKey = "level"

	zapLogger, err := config.Build()
	if err != nil {
		return err
	}

	Logger = zapLogger.Sugar()
	return nil
}

// InitWithLevelAndHTTP initializes the global zap logger with specific log level and HTTP output
func InitWithHTTP(level string, debugMode bool) error {
	// Console encoder configuration
	consoleEncoderConfig := zap.NewProductionEncoderConfig()
	consoleEncoderConfig.TimeKey = "timestamp"
	consoleEncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	consoleEncoderConfig.MessageKey = "message"
	consoleEncoderConfig.LevelKey = "level"
	consoleEncoder := zapcore.NewJSONEncoder(consoleEncoderConfig)

	// HTTP encoder configuration
	httpEncoderConfig := zap.NewProductionEncoderConfig()
	httpEncoderConfig.TimeKey = "timestamp"
	httpEncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	httpEncoderConfig.MessageKey = "message"
	httpEncoderConfig.LevelKey = "level"
	httpEncoder := zapcore.NewJSONEncoder(httpEncoderConfig)

	// Create HTTP writer and store reference for cleanup
	httpWriter = NewHTTPWriter()
	parsedLevel, err := zap.ParseAtomicLevel(level)
	if err != nil {
		parsedLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	}
	httpLevel := zap.InfoLevel
	if debugMode {
		httpLevel = zap.DebugLevel
	}
	// Create the tee core with both console and HTTP output
	core := zapcore.NewTee(
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), parsedLevel),
		zapcore.NewCore(httpEncoder, zapcore.AddSync(httpWriter), httpLevel),
	)

	zapLogger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	Logger = zapLogger.Sugar()
	return nil
}

// GetLogger returns the global logger instance
func GetLogger() *zap.SugaredLogger {
	if Logger == nil {
		// Fallback initialization if not already initialized
		if err := Init("info", false); err != nil {
			panic("Failed to initialize logger: " + err.Error())
		}
	}
	return Logger
}

// Info logs an info message
func Info(args ...interface{}) {
	GetLogger().Info(args...)
}

// Infof logs an info message with formatting
func Infof(template string, args ...interface{}) {
	GetLogger().Infof(template, args...)
}

// Infow logs an info message with formatting
func Infow(message string, args ...interface{}) {
	GetLogger().Infow(message, args...)
}

// Debug logs a debug message
func Debug(args ...interface{}) {
	GetLogger().Debug(args...)
}

// Debugf logs a debug message with formatting
func Debugf(template string, args ...interface{}) {
	GetLogger().Debugf(template, args...)
}

// Debugw logs a debug message with formatting
func Debugw(message string, args ...interface{}) {
	GetLogger().Debugw(message, args...)
}

// Error logs an error message
func Error(args ...interface{}) {
	GetLogger().Error(args...)
}

// Errorf logs an error message with formatting
func Errorf(template string, args ...interface{}) {
	GetLogger().Errorf(template, args...)
}

// Errorw logs an error message with formatting
func Errorw(message string, args ...interface{}) {
	GetLogger().Errorw(message, args...)
}

// Warning logs a warning message
func Warning(args ...interface{}) {
	GetLogger().Warn(args...)
}

// Warningf logs a warning message with formatting
func Warningf(template string, args ...interface{}) {
	GetLogger().Warnf(template, args...)
}

// Warnw logs a warning message with formatting
func Warnw(message string, args ...interface{}) {
	GetLogger().Warnw(message, args...)
}

// Fatal logs a fatal message and exits
func Fatal(args ...interface{}) {
	GetLogger().Fatal(args...)
}

// Fatalf logs a fatal message with formatting and exits
func Fatalf(template string, args ...interface{}) {
	GetLogger().Fatalf(template, args...)
}

// Fatalw logs a fatal message with formatting and exits
func Fatalw(message string, args ...interface{}) {
	GetLogger().Fatalw(message, args...)
}

// Shutdown gracefully shuts down the logger and flushes any pending logs
func Shutdown() error {
	if httpWriter != nil {
		return httpWriter.Close()
	}
	Logger.Sync()
	return nil
}

func LogPanic(r interface{}) {
	if r == http.ErrAbortHandler {
		// honor the http.ErrAbortHandler sentinel panic value:
		//   ErrAbortHandler is a sentinel panic value to abort a handler.
		//   While any panic from ServeHTTP aborts the response to the client,
		//   panicking with ErrAbortHandler also suppresses logging of a stack trace to the server's error log.
		return
	}

	// Same as stdlib http server code. Manually allocate stack trace buffer size
	// to prevent excessively large logs
	const size = 64 << 10
	stacktrace := make([]byte, size)
	stacktrace = stacktrace[:runtime.Stack(stacktrace, false)]
	GetLogger().Errorw("Observed a panic", "panic", r, "stacktrace", string(stacktrace))
	Logger.Sync()
}
