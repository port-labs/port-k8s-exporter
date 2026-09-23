package event_handler

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/port-labs/port-k8s-exporter/pkg/logger"
)

// ScheduledListener is used when the Port integration API is skipped. It does
// not poll Port for configuration changes, so resyncs are driven only by the
// configured resync interval and the initial resync.
type ScheduledListener struct{}

func NewScheduledListener() *ScheduledListener {
	return &ScheduledListener{}
}

func (l *ScheduledListener) Run(resync func()) error {
	logger.Info("Port integration API is disabled (skip-integration), running without config-change detection")
	logger.Info("Resyncs will run on the configured resync interval and on startup only")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	logger.Infof("Received signal %v: terminating", sig)
	logger.Shutdown()

	return nil
}
