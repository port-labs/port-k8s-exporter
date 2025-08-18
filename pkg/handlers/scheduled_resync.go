package handlers

import (
	"sync"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
)

// ScheduledResyncManager manages the scheduled resync operations
type ScheduledResyncManager struct {
	applicationConfig *port.Config
	k8sClient         *k8s.Client
	portClient        *cli.PortClient
	resyncInterval    time.Duration
	stopCh            chan struct{}
	lastResyncTime    time.Time
	resyncMutex       sync.RWMutex
	isRunning         bool
	runningMutex      sync.Mutex
}

// NewScheduledResyncManager creates a new scheduled resync manager
func NewScheduledResyncManager(applicationConfig *port.Config, k8sClient *k8s.Client, portClient *cli.PortClient, resyncIntervalMinutes uint) *ScheduledResyncManager {
	return &ScheduledResyncManager{
		applicationConfig: applicationConfig,
		k8sClient:         k8sClient,
		portClient:        portClient,
		resyncInterval:    time.Minute * time.Duration(resyncIntervalMinutes),
		stopCh:            make(chan struct{}),
		lastResyncTime:    time.Now(),
	}
}

// Start begins the scheduled resync process
func (m *ScheduledResyncManager) Start() {
	m.runningMutex.Lock()
	if m.isRunning {
		m.runningMutex.Unlock()
		logger.Warning("Scheduled resync manager is already running")
		return
	}
	m.isRunning = true
	m.runningMutex.Unlock()

	// Initialize last resync time
	m.resyncMutex.Lock()
	m.lastResyncTime = time.Now()
	m.resyncMutex.Unlock()

	// Start the resync ticker goroutine
	go m.runResyncTicker()

	// Start the health check goroutine
	go m.runHealthCheck()

	logger.Infof("Scheduled resync manager started with interval: %v", m.resyncInterval)
}

// Stop gracefully stops the scheduled resync process
func (m *ScheduledResyncManager) Stop() {
	m.runningMutex.Lock()
	defer m.runningMutex.Unlock()

	if !m.isRunning {
		logger.Warning("Scheduled resync manager is not running")
		return
	}

	logger.Info("Stopping scheduled resync manager")
	close(m.stopCh)
	m.isRunning = false
}

// GetLastResyncTime returns the last resync time
func (m *ScheduledResyncManager) GetLastResyncTime() time.Time {
	m.resyncMutex.RLock()
	defer m.resyncMutex.RUnlock()
	return m.lastResyncTime
}

// runResyncTicker runs the main resync ticker loop
func (m *ScheduledResyncManager) runResyncTicker() {
	// Add panic recovery to prevent goroutine from dying silently
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("Panic in resync ticker goroutine: %v", r)
			logger.Error("Resync ticker goroutine crashed and will not restart automatically")
		}
	}()

	ticker := time.NewTicker(m.resyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.performScheduledResync()
		case <-m.stopCh:
			logger.Info("Stopping resync ticker")
			return
		}
	}
}

// performScheduledResync executes a single scheduled resync operation
func (m *ScheduledResyncManager) performScheduledResync() {
	logger.Infof("Starting scheduled resync (interval: %v)", m.resyncInterval)

	// Update last resync time
	m.resyncMutex.Lock()
	m.lastResyncTime = time.Now()
	m.resyncMutex.Unlock()

	// Run resync in a separate goroutine with timeout and panic recovery
	done := make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("Panic during scheduled resync: %v", r)
			}
			done <- true
		}()

		if err := RunResync(m.applicationConfig, m.k8sClient, m.portClient, SCHEDULED_RESYNC); err != nil {
			logger.Errorf("Error during scheduled resync: %v", err)
		}
	}()

	// Wait for resync to complete or timeout
	// Timeout is set to slightly less than the resync interval to avoid overlap
	timeout := m.resyncInterval - time.Second
	if timeout < time.Second {
		timeout = time.Second
	}

	select {
	case <-done:
		logger.Info("Scheduled resync completed")
	case <-time.After(timeout):
		// Timeout just before next tick to avoid overlap
		logger.Error("Scheduled resync timed out, will retry on next interval")
	}
}

// runHealthCheck monitors the health of the resync ticker
func (m *ScheduledResyncManager) runHealthCheck() {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("Panic in resync health check goroutine: %v", r)
		}
	}()

	// Check every 5 minutes if the ticker is still working
	healthTicker := time.NewTicker(5 * time.Minute)
	defer healthTicker.Stop()

	for {
		select {
		case <-healthTicker.C:
			m.checkResyncHealth()
		case <-m.stopCh:
			logger.Info("Stopping resync health check")
			return
		}
	}
}

// checkResyncHealth checks if resyncs are happening as expected
func (m *ScheduledResyncManager) checkResyncHealth() {
	m.resyncMutex.RLock()
	timeSinceLastResync := time.Since(m.lastResyncTime)
	m.resyncMutex.RUnlock()

	// Allow 2x the interval as a grace period
	maxAllowedTime := m.resyncInterval * 2
	if timeSinceLastResync > maxAllowedTime {
		logger.Errorf("WARNING: No resync has occurred for %v (expected interval: %v). The ticker may have stopped working!",
			timeSinceLastResync, m.resyncInterval)
	}
}

// IsRunning returns whether the scheduled resync manager is running
func (m *ScheduledResyncManager) IsRunning() bool {
	m.runningMutex.Lock()
	defer m.runningMutex.Unlock()
	return m.isRunning
}
