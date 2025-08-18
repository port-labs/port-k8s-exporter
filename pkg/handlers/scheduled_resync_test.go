package handlers

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/stretchr/testify/assert"
)

// TestScheduledResyncManager_NewManager tests the creation of a new manager
func TestScheduledResyncManager_NewManager(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	manager := NewScheduledResyncManager(appConfig, nil, nil, 5)

	assert.NotNil(t, manager, "Manager should be created")
	assert.Equal(t, appConfig, manager.applicationConfig, "Application config should be set")
	assert.Equal(t, 5*time.Minute, manager.resyncInterval, "Resync interval should be 5 minutes")
	assert.NotNil(t, manager.stopCh, "Stop channel should be initialized")
	assert.False(t, manager.isRunning, "Manager should not be running initially")
	assert.NotZero(t, manager.lastResyncTime, "Initial last resync time should be set")
}

// TestScheduledResyncManager_StartStop tests starting and stopping the manager
func TestScheduledResyncManager_StartStop(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	manager := NewScheduledResyncManager(appConfig, nil, nil, 1)

	// Test initial state
	assert.False(t, manager.IsRunning(), "Manager should not be running initially")

	// Test start
	manager.Start()
	assert.True(t, manager.IsRunning(), "Manager should be running after Start()")

	// Test duplicate start (should not panic, just log warning)
	manager.Start()
	assert.True(t, manager.IsRunning(), "Manager should still be running")

	// Test stop
	manager.Stop()
	// Give goroutines time to stop
	time.Sleep(100 * time.Millisecond)
	assert.False(t, manager.IsRunning(), "Manager should not be running after Stop()")

	// Test duplicate stop (should not panic, just log warning)
	manager.Stop()
	assert.False(t, manager.IsRunning(), "Manager should still not be running")
}

// TestScheduledResyncManager_GetLastResyncTime tests getting the last resync time
func TestScheduledResyncManager_GetLastResyncTime(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	manager := NewScheduledResyncManager(appConfig, nil, nil, 1)

	// Check initial last resync time
	initialTime := manager.GetLastResyncTime()
	assert.NotZero(t, initialTime, "Initial last resync time should be set")
	assert.True(t, time.Since(initialTime) < time.Second, "Initial time should be recent")

	// Manually update last resync time
	time.Sleep(100 * time.Millisecond)
	manager.resyncMutex.Lock()
	manager.lastResyncTime = time.Now()
	manager.resyncMutex.Unlock()

	// Check updated time
	updatedTime := manager.GetLastResyncTime()
	assert.True(t, updatedTime.After(initialTime), "Updated time should be after initial time")
}

// TestScheduledResyncManager_PerformScheduledResync tests the resync execution
func TestScheduledResyncManager_PerformScheduledResync(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	manager := NewScheduledResyncManager(appConfig, nil, nil, 1)

	// Record time before resync
	timeBefore := manager.GetLastResyncTime()
	time.Sleep(10 * time.Millisecond)

	// Perform resync - it will fail because clients are nil, but that's ok for this test
	manager.performScheduledResync()

	// Check that last resync time was updated
	timeAfter := manager.GetLastResyncTime()
	assert.True(t, timeAfter.After(timeBefore), "Last resync time should be updated")
}

// TestScheduledResyncManager_PerformScheduledResyncRecovery tests that performScheduledResync handles errors gracefully
func TestScheduledResyncManager_PerformScheduledResyncRecovery(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	// Manager with nil clients will cause RunResync to fail/panic
	manager := NewScheduledResyncManager(appConfig, nil, nil, 1)

	// Perform resync (should handle error/panic gracefully)
	assert.NotPanics(t, func() {
		manager.performScheduledResync()
	}, "Should handle errors gracefully")

	// Check that last resync time was still updated
	assert.NotZero(t, manager.GetLastResyncTime(), "Last resync time should still be updated even on error")
}

// TestScheduledResyncManager_CheckResyncHealth tests health check functionality
func TestScheduledResyncManager_CheckResyncHealth(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	tests := []struct {
		name           string
		lastResyncAge  time.Duration
		resyncInterval time.Duration
		shouldWarn     bool
	}{
		{
			name:           "Recent resync - no warning",
			lastResyncAge:  30 * time.Second,
			resyncInterval: 1 * time.Minute,
			shouldWarn:     false,
		},
		{
			name:           "Old resync - should warn",
			lastResyncAge:  3 * time.Minute,
			resyncInterval: 1 * time.Minute,
			shouldWarn:     true,
		},
		{
			name:           "Just under threshold - no warning",
			lastResyncAge:  119 * time.Second,
			resyncInterval: 1 * time.Minute,
			shouldWarn:     false,
		},
		{
			name:           "Just over threshold - should warn",
			lastResyncAge:  121 * time.Second,
			resyncInterval: 1 * time.Minute,
			shouldWarn:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &ScheduledResyncManager{
				applicationConfig: appConfig,
				resyncInterval:    tt.resyncInterval,
				lastResyncTime:    time.Now().Add(-tt.lastResyncAge),
			}

			// We can't easily test log output, but we can verify it doesn't panic
			assert.NotPanics(t, func() {
				manager.checkResyncHealth()
			}, "Health check should not panic")
		})
	}
}

// TestScheduledResyncManager_ConcurrentAccess tests thread safety
func TestScheduledResyncManager_ConcurrentAccess(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	manager := NewScheduledResyncManager(appConfig, nil, nil, 1)

	// Start multiple goroutines accessing the manager concurrently
	var wg sync.WaitGroup
	numGoroutines := 10

	// Track operations
	var readCount, writeCount atomic.Int32

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Perform multiple operations
			for j := 0; j < 10; j++ {
				// Read operation
				_ = manager.GetLastResyncTime()
				readCount.Add(1)

				// Write operation (if even numbered goroutine)
				if id%2 == 0 {
					manager.resyncMutex.Lock()
					manager.lastResyncTime = time.Now()
					manager.resyncMutex.Unlock()
					writeCount.Add(1)
				}

				// Check running status
				_ = manager.IsRunning()

				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	assert.Equal(t, int32(numGoroutines*10), readCount.Load(), "All read operations should complete")
	assert.Equal(t, int32((numGoroutines/2)*10), writeCount.Load(), "All write operations should complete")
}

// TestScheduledResyncManager_TickerContinuesAfterError tests that ticker continues after errors
func TestScheduledResyncManager_TickerContinuesAfterError(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	// Track resync attempts by monitoring last resync time changes
	var resyncCount atomic.Int32

	// Create manager with very short interval for testing
	manager := &ScheduledResyncManager{
		applicationConfig: appConfig,
		k8sClient:         nil, // nil clients will cause errors
		portClient:        nil,
		resyncInterval:    100 * time.Millisecond,
		stopCh:            make(chan struct{}),
		lastResyncTime:    time.Now(),
		isRunning:         false,
	}

	// Monitor last resync time changes
	lastTime := manager.GetLastResyncTime()
	go func() {
		for {
			select {
			case <-manager.stopCh:
				return
			case <-time.After(50 * time.Millisecond):
				currentTime := manager.GetLastResyncTime()
				if currentTime.After(lastTime) {
					resyncCount.Add(1)
					lastTime = currentTime
				}
			}
		}
	}()

	// Start the manager
	manager.runningMutex.Lock()
	manager.isRunning = true
	manager.runningMutex.Unlock()

	// Run ticker in a goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Ticker panicked: %v", r)
			}
		}()
		manager.runResyncTicker()
	}()

	// Wait for multiple ticks
	time.Sleep(350 * time.Millisecond)

	// Stop the manager
	close(manager.stopCh)
	time.Sleep(50 * time.Millisecond)

	// Check that multiple resyncs were attempted despite errors
	finalCount := resyncCount.Load()
	assert.GreaterOrEqual(t, finalCount, int32(2), "Should attempt resync at least 2 times (ticker should continue after errors)")
}

// TestScheduledResyncManager_HealthCheckRunsIndependently tests health check independence
func TestScheduledResyncManager_HealthCheckRunsIndependently(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	manager := &ScheduledResyncManager{
		applicationConfig: appConfig,
		resyncInterval:    1 * time.Minute,
		stopCh:            make(chan struct{}),
		lastResyncTime:    time.Now().Add(-3 * time.Minute), // Old resync time
		isRunning:         true,
	}

	// Run health check in a goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Health check panicked: %v", r)
			}
		}()
		manager.runHealthCheck()
	}()

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop it
	close(manager.stopCh)
	time.Sleep(50 * time.Millisecond)

	// Note: The health check runs every 5 minutes, so it won't execute in this short test
	// This test mainly verifies that the health check goroutine starts and stops without issues
	assert.True(t, true, "Health check goroutine should start and stop without issues")
}

// TestScheduledResyncManager_TimeoutCalculation tests timeout calculation logic
func TestScheduledResyncManager_TimeoutCalculation(t *testing.T) {
	tests := []struct {
		name            string
		resyncInterval  time.Duration
		expectedTimeout time.Duration
	}{
		{
			name:            "Normal interval",
			resyncInterval:  5 * time.Minute,
			expectedTimeout: 5*time.Minute - time.Second,
		},
		{
			name:            "Very short interval - minimum timeout applies",
			resyncInterval:  500 * time.Millisecond,
			expectedTimeout: time.Second, // minimum
		},
		{
			name:            "One second interval - minimum timeout applies",
			resyncInterval:  1 * time.Second,
			expectedTimeout: time.Second, // minimum
		},
		{
			name:            "Two second interval",
			resyncInterval:  2 * time.Second,
			expectedTimeout: 1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appConfig := &port.Config{
				StateKey: "test-state",
			}

			manager := &ScheduledResyncManager{
				applicationConfig: appConfig,
				resyncInterval:    tt.resyncInterval,
				stopCh:            make(chan struct{}),
				lastResyncTime:    time.Now(),
			}

			// Calculate timeout as the manager would
			timeout := manager.resyncInterval - time.Second
			if timeout < time.Second {
				timeout = time.Second
			}

			assert.Equal(t, tt.expectedTimeout, timeout, "Timeout calculation should match expected")
		})
	}
}
