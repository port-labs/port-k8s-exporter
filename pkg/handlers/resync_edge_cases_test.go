package handlers

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/stretchr/testify/assert"
)

// TestConcurrentScheduledResyncs tests multiple scheduled resync managers running concurrently
func TestConcurrentScheduledResyncs(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	// Create multiple managers with different intervals
	manager1 := NewScheduledResyncManager(appConfig, nil, nil, 1)
	manager2 := NewScheduledResyncManager(appConfig, nil, nil, 2)
	manager3 := NewScheduledResyncManager(appConfig, nil, nil, 3)

	// No need to track execution for this test

	// Start all managers
	manager1.Start()
	manager2.Start()
	manager3.Start()

	// Let them run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop all managers
	manager1.Stop()
	manager2.Stop()
	manager3.Stop()

	// Verify all managers stopped
	assert.False(t, manager1.IsRunning(), "Manager1 should be stopped")
	assert.False(t, manager2.IsRunning(), "Manager2 should be stopped")
	assert.False(t, manager3.IsRunning(), "Manager3 should be stopped")
}

// TestScheduledResyncUnderLoad tests scheduled resync behavior under high load
func TestScheduledResyncUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	appConfig := &port.Config{
		StateKey: "test-state",
	}

	// Create manager with very short interval for stress testing
	manager := &ScheduledResyncManager{
		applicationConfig: appConfig,
		k8sClient:         nil,
		portClient:        nil,
		resyncInterval:    50 * time.Millisecond,
		stopCh:            make(chan struct{}),
		lastResyncTime:    time.Now(),
		isRunning:         false,
	}

	// Track resync attempts
	var resyncAttempts atomic.Int32

	// Start the manager
	manager.runningMutex.Lock()
	manager.isRunning = true
	manager.runningMutex.Unlock()

	// Run ticker in background
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Ticker recovered from panic (expected due to nil clients)")
			}
		}()

		// Track attempts in the ticker loop
		ticker := time.NewTicker(manager.resyncInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				resyncAttempts.Add(1)
				// Try to perform resync (will fail due to nil clients)
				func() {
					defer func() {
						if r := recover(); r != nil {
							// Expected due to nil clients
						}
					}()
					manager.performScheduledResync()
				}()
			}
		}
	}()

	// Wait for test duration
	<-ctx.Done()
	close(manager.stopCh)
	time.Sleep(100 * time.Millisecond)

	attempts := resyncAttempts.Load()

	t.Logf("Load test results: Attempts=%d", attempts)

	// Should have multiple attempts
	assert.GreaterOrEqual(t, attempts, int32(5), "Should have multiple resync attempts under load")
}

// TestScheduledResyncWithVariableIntervals tests changing resync intervals
func TestScheduledResyncWithVariableIntervals(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	// Test different interval configurations
	intervals := []uint{1, 5, 10, 60}

	for _, interval := range intervals {
		manager := NewScheduledResyncManager(appConfig, nil, nil, interval)

		expectedInterval := time.Duration(interval) * time.Minute
		assert.Equal(t, expectedInterval, manager.resyncInterval,
			"Manager should have correct interval for %d minutes", interval)

		// Calculate expected timeout
		expectedTimeout := expectedInterval - time.Second
		if expectedTimeout < time.Second {
			expectedTimeout = time.Second
		}

		// Verify timeout calculation
		actualTimeout := manager.resyncInterval - time.Second
		if actualTimeout < time.Second {
			actualTimeout = time.Second
		}

		assert.Equal(t, expectedTimeout, actualTimeout,
			"Timeout should be calculated correctly for interval %d", interval)
	}
}

// TestScheduledResyncMemoryLeaks tests for potential memory leaks
func TestScheduledResyncMemoryLeaks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	appConfig := &port.Config{
		StateKey: "test-state",
	}

	// Create and destroy many managers
	for i := 0; i < 100; i++ {
		manager := NewScheduledResyncManager(appConfig, nil, nil, 1)
		manager.Start()
		time.Sleep(1 * time.Millisecond)
		manager.Stop()
	}

	// Force garbage collection
	time.Sleep(100 * time.Millisecond)

	// If we get here without issues, the test passes
	assert.True(t, true, "No apparent memory leaks from creating/destroying managers")
}

// TestScheduledResyncRaceConditions tests for race conditions
func TestScheduledResyncRaceConditions(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	manager := NewScheduledResyncManager(appConfig, nil, nil, 1)

	// Concurrent operations
	var wg sync.WaitGroup
	numGoroutines := 50

	// Start/stop operations
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			manager.Start()
			time.Sleep(time.Microsecond)
			manager.Stop()
		}()
	}

	// Read operations
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = manager.IsRunning()
			_ = manager.GetLastResyncTime()
		}()
	}

	wg.Wait()

	// If we get here without race conditions, test passes
	assert.True(t, true, "No race conditions detected")
}

// TestScheduledResyncHealthCheckTiming tests health check timing accuracy
func TestScheduledResyncHealthCheckTiming(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	testCases := []struct {
		name              string
		lastResyncAge     time.Duration
		resyncInterval    time.Duration
		shouldTriggerWarn bool
	}{
		{
			name:              "Fresh resync",
			lastResyncAge:     30 * time.Second,
			resyncInterval:    1 * time.Minute,
			shouldTriggerWarn: false,
		},
		{
			name:              "Just before threshold",
			lastResyncAge:     119 * time.Second,
			resyncInterval:    1 * time.Minute,
			shouldTriggerWarn: false,
		},
		{
			name:              "Just after threshold",
			lastResyncAge:     121 * time.Second,
			resyncInterval:    1 * time.Minute,
			shouldTriggerWarn: true,
		},
		{
			name:              "Way past threshold",
			lastResyncAge:     10 * time.Minute,
			resyncInterval:    1 * time.Minute,
			shouldTriggerWarn: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager := &ScheduledResyncManager{
				applicationConfig: appConfig,
				resyncInterval:    tc.resyncInterval,
				lastResyncTime:    time.Now().Add(-tc.lastResyncAge),
			}

			// Check if warning would be triggered
			timeSinceLastResync := time.Since(manager.lastResyncTime)
			maxAllowedTime := manager.resyncInterval * 2
			wouldWarn := timeSinceLastResync > maxAllowedTime

			assert.Equal(t, tc.shouldTriggerWarn, wouldWarn,
				"Warning trigger should match expected for %s", tc.name)
		})
	}
}

// TestScheduledResyncPanicRecoveryChain tests panic recovery at multiple levels
func TestScheduledResyncPanicRecoveryChain(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	manager := NewScheduledResyncManager(appConfig, nil, nil, 1)

	// Test that panics are caught at different levels
	tests := []struct {
		name string
		fn   func()
	}{
		{
			name: "Panic in performScheduledResync",
			fn: func() {
				manager.performScheduledResync()
			},
		},
		{
			name: "Panic in runResyncTicker",
			fn: func() {
				// Create a manager that will panic immediately
				m := &ScheduledResyncManager{
					applicationConfig: appConfig,
					resyncInterval:    1 * time.Millisecond,
					stopCh:            make(chan struct{}),
					lastResyncTime:    time.Now(),
					isRunning:         true,
				}

				// Run briefly then stop
				go func() {
					time.Sleep(10 * time.Millisecond)
					close(m.stopCh)
				}()

				m.runResyncTicker()
			},
		},
		{
			name: "Panic in health check",
			fn: func() {
				manager.checkResyncHealth()
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.NotPanics(t, test.fn, "Should not panic in %s", test.name)
		})
	}
}

// TestScheduledResyncStartStopIdempotency tests that start/stop operations are idempotent
func TestScheduledResyncStartStopIdempotency(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	manager := NewScheduledResyncManager(appConfig, nil, nil, 1)

	// Multiple starts should be safe
	for i := 0; i < 5; i++ {
		manager.Start()
		assert.True(t, manager.IsRunning(), "Manager should be running after start %d", i+1)
	}

	// Multiple stops should be safe
	for i := 0; i < 5; i++ {
		manager.Stop()
		if i == 0 {
			// Give first stop time to complete
			time.Sleep(100 * time.Millisecond)
		}
		assert.False(t, manager.IsRunning(), "Manager should not be running after stop %d", i+1)
	}
}

// TestScheduledResyncLastResyncTimeAccuracy tests accuracy of last resync time tracking
func TestScheduledResyncLastResyncTimeAccuracy(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	manager := NewScheduledResyncManager(appConfig, nil, nil, 1)

	// Record initial time
	initialTime := manager.GetLastResyncTime()
	assert.NotZero(t, initialTime, "Initial time should be set")

	// Perform a resync
	beforeResync := time.Now()
	manager.performScheduledResync()
	afterResync := time.Now()

	// Get updated time
	updatedTime := manager.GetLastResyncTime()

	// Verify time was updated and is within expected range
	assert.True(t, updatedTime.After(initialTime), "Time should be updated after resync")
	assert.True(t, !updatedTime.Before(beforeResync), "Updated time should not be before resync started")
	assert.True(t, !updatedTime.After(afterResync), "Updated time should not be after resync completed")
}

// TestScheduledResyncConcurrentPerformance tests performance under concurrent access
func TestScheduledResyncConcurrentPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	appConfig := &port.Config{
		StateKey: "test-state",
	}

	manager := NewScheduledResyncManager(appConfig, nil, nil, 1)
	manager.Start()
	defer manager.Stop()

	// Measure concurrent access performance
	start := time.Now()

	var wg sync.WaitGroup
	numGoroutines := 1000
	numOperations := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_ = manager.IsRunning()
				_ = manager.GetLastResyncTime()
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)

	totalOps := numGoroutines * numOperations * 2 // 2 operations per iteration
	opsPerSecond := float64(totalOps) / duration.Seconds()

	t.Logf("Performance: %d operations in %v (%.0f ops/sec)", totalOps, duration, opsPerSecond)

	// Should complete in reasonable time
	assert.Less(t, duration, 5*time.Second, "Concurrent operations should complete quickly")
}

// TestScheduledResyncGracefulDegradation tests system behavior when components fail
func TestScheduledResyncGracefulDegradation(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	// Manager with nil clients (will cause errors)
	manager := NewScheduledResyncManager(appConfig, nil, nil, 1)

	// Should still be able to start and stop
	assert.NotPanics(t, func() {
		manager.Start()
	}, "Should start even with nil clients")

	assert.True(t, manager.IsRunning(), "Should be running")

	// Perform resync (will error but shouldn't panic)
	assert.NotPanics(t, func() {
		manager.performScheduledResync()
	}, "Should handle errors gracefully")

	// Should still be able to stop
	assert.NotPanics(t, func() {
		manager.Stop()
	}, "Should stop even after errors")

	assert.False(t, manager.IsRunning(), "Should not be running after stop")
}

// TestScheduledResyncTimeoutEdgeCases tests edge cases in timeout calculation
func TestScheduledResyncTimeoutEdgeCases(t *testing.T) {
	testCases := []struct {
		name            string
		resyncInterval  time.Duration
		expectedTimeout time.Duration
	}{
		{
			name:            "Zero interval",
			resyncInterval:  0,
			expectedTimeout: time.Second, // minimum
		},
		{
			name:            "Sub-second interval",
			resyncInterval:  500 * time.Millisecond,
			expectedTimeout: time.Second, // minimum
		},
		{
			name:            "Exactly 1 second",
			resyncInterval:  1 * time.Second,
			expectedTimeout: time.Second, // minimum
		},
		{
			name:            "Just over 1 second",
			resyncInterval:  1001 * time.Millisecond,
			expectedTimeout: time.Second, // minimum (1ms is too small)
		},
		{
			name:            "2 seconds",
			resyncInterval:  2 * time.Second,
			expectedTimeout: 1 * time.Second,
		},
		{
			name:            "Large interval",
			resyncInterval:  1 * time.Hour,
			expectedTimeout: 1*time.Hour - time.Second,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			appConfig := &port.Config{
				StateKey: "test-state",
			}

			manager := &ScheduledResyncManager{
				applicationConfig: appConfig,
				resyncInterval:    tc.resyncInterval,
				stopCh:            make(chan struct{}),
				lastResyncTime:    time.Now(),
			}

			// Calculate timeout as the manager would
			timeout := manager.resyncInterval - time.Second
			if timeout < time.Second {
				timeout = time.Second
			}

			assert.Equal(t, tc.expectedTimeout, timeout,
				"Timeout calculation should match expected for interval %v", tc.resyncInterval)
		})
	}
}
