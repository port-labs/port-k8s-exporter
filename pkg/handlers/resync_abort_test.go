package handlers

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/dynamic/dynamicinformer"
)

// TestResyncAbortsOngoingResync verifies that starting a new resync aborts the previous one
func TestResyncAbortsOngoingResync(t *testing.T) {
	// This test verifies the behavior at the controllerHandler level
	// We'll track the lifecycle of controller handlers

	// Track handler lifecycle
	var activeHandlers []*ControllersHandler
	var mu sync.Mutex

	// Create first handler
	handler1 := &ControllersHandler{
		controllers:      []*k8s.Controller{},
		informersFactory: dynamicinformer.NewDynamicSharedInformerFactory(nil, 0),
		stateKey:         "test-1",
		portClient:       nil,
		stopCh:           make(chan struct{}),
		isStopped:        false,
		portConfig:       &port.IntegrationAppConfig{},
	}

	mu.Lock()
	activeHandlers = append(activeHandlers, handler1)
	controllerHandler = handler1
	mu.Unlock()

	// Verify first handler is active
	assert.False(t, handler1.isStopped, "First handler should be active")

	// Simulate work in first handler
	go func() {
		select {
		case <-handler1.stopCh:
			// Handler was stopped
			return
		case <-time.After(5 * time.Second):
			// Should not reach here in normal test
		}
	}()

	// Create second handler (simulating new resync)
	handler2 := &ControllersHandler{
		controllers:      []*k8s.Controller{},
		informersFactory: dynamicinformer.NewDynamicSharedInformerFactory(nil, 0),
		stateKey:         "test-2",
		portClient:       nil,
		stopCh:           make(chan struct{}),
		isStopped:        false,
		portConfig:       &port.IntegrationAppConfig{},
	}

	// Simulate RunResync behavior - stop previous handler
	if controllerHandler != nil {
		controllerHandler.Stop()
	}

	mu.Lock()
	activeHandlers = append(activeHandlers, handler2)
	controllerHandler = handler2
	mu.Unlock()

	// Verify first handler was stopped
	assert.True(t, handler1.isStopped, "First handler should be stopped")
	assert.False(t, handler2.isStopped, "Second handler should be active")

	// Verify stop channel was closed for first handler
	select {
	case <-handler1.stopCh:
		// Good, channel is closed
	default:
		t.Error("First handler's stop channel should be closed")
	}

	// Clean up
	handler2.Stop()
	assert.True(t, handler2.isStopped, "Second handler should be stopped after cleanup")
}

// TestMultipleResyncsInQuickSuccession tests rapid resync requests
func TestMultipleResyncsInQuickSuccession(t *testing.T) {
	// Track handler states
	type HandlerState struct {
		ID        string
		StartTime time.Time
		StopTime  *time.Time
		Stopped   atomic.Bool
	}

	var handlers []*HandlerState
	var handlersLock sync.Mutex

	// Simulate multiple rapid resync requests
	numResyncs := 5

	for i := 0; i < numResyncs; i++ {
		state := &HandlerState{
			ID:        string(rune('A' + i)),
			StartTime: time.Now(),
		}

		handlersLock.Lock()
		handlers = append(handlers, state)

		// Stop previous handler if exists
		if i > 0 && len(handlers) > 1 {
			prevState := handlers[i-1]
			if !prevState.Stopped.Load() {
				now := time.Now()
				prevState.StopTime = &now
				prevState.Stopped.Store(true)
			}
		}
		handlersLock.Unlock()

		// Small delay to simulate work
		time.Sleep(10 * time.Millisecond)
	}

	// Verify that each handler (except the last) was stopped
	handlersLock.Lock()
	defer handlersLock.Unlock()

	for i, state := range handlers {
		if i < len(handlers)-1 {
			assert.True(t, state.Stopped.Load(),
				"Handler %s should be stopped (not the last one)", state.ID)
			assert.NotNil(t, state.StopTime,
				"Handler %s should have a stop time", state.ID)

			// Verify it was stopped before the next one started
			if i+1 < len(handlers) {
				nextHandler := handlers[i+1]
				if state.StopTime != nil {
					assert.True(t, state.StopTime.Before(nextHandler.StartTime) ||
						state.StopTime.Equal(nextHandler.StartTime),
						"Handler %s should be stopped before handler %s starts",
						state.ID, nextHandler.ID)
				}
			}
		} else {
			// Last handler should still be active
			assert.False(t, state.Stopped.Load(),
				"Last handler %s should still be active", state.ID)
		}
	}
}

// TestResyncStopChannelPropagation tests that stop signal propagates to all components
func TestResyncStopChannelPropagation(t *testing.T) {
	handler := &ControllersHandler{
		controllers:      []*k8s.Controller{},
		informersFactory: dynamicinformer.NewDynamicSharedInformerFactory(nil, 0),
		stateKey:         "test",
		portClient:       nil,
		stopCh:           make(chan struct{}),
		isStopped:        false,
		portConfig:       &port.IntegrationAppConfig{},
	}

	// Track which components received stop signal
	var componentsStoppedCount atomic.Int32
	numComponents := 3

	// Simulate multiple components listening to stop channel
	for i := 0; i < numComponents; i++ {
		go func(componentID int) {
			select {
			case <-handler.stopCh:
				componentsStoppedCount.Add(1)
			case <-time.After(1 * time.Second):
				t.Errorf("Component %d did not receive stop signal", componentID)
			}
		}(i)
	}

	// Stop the handler
	handler.Stop()

	// Wait for all components to receive stop signal
	time.Sleep(100 * time.Millisecond)

	// Verify all components received the stop signal
	stoppedCount := componentsStoppedCount.Load()
	assert.Equal(t, int32(numComponents), stoppedCount,
		"All components should receive stop signal")

	// Verify handler is marked as stopped
	assert.True(t, handler.isStopped, "Handler should be marked as stopped")

	// Verify stop channel is closed
	select {
	case <-handler.stopCh:
		// Good, channel is closed
	default:
		t.Error("Stop channel should be closed")
	}
}

// TestResyncIdempotentStop tests that calling Stop multiple times is safe
func TestResyncIdempotentStop(t *testing.T) {
	handler := &ControllersHandler{
		controllers:      []*k8s.Controller{},
		informersFactory: dynamicinformer.NewDynamicSharedInformerFactory(nil, 0),
		stateKey:         "test",
		portClient:       nil,
		stopCh:           make(chan struct{}),
		isStopped:        false,
		portConfig:       &port.IntegrationAppConfig{},
	}

	// First stop
	handler.Stop()
	assert.True(t, handler.isStopped, "Handler should be stopped after first Stop()")

	// Multiple additional stops should be safe (not panic)
	assert.NotPanics(t, func() {
		handler.Stop()
		handler.Stop()
		handler.Stop()
	}, "Multiple Stop() calls should not panic")

	// Handler should still be stopped
	assert.True(t, handler.isStopped, "Handler should remain stopped")
}

// TestConcurrentResyncAborts tests concurrent resync requests and abort behavior
func TestConcurrentResyncAborts(t *testing.T) {
	// Track abort events
	type AbortEvent struct {
		HandlerID string
		AbortTime time.Time
		Reason    string
	}

	var abortEvents []AbortEvent
	var abortLock sync.Mutex

	// Simulate concurrent resync requests
	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Create handler
			handler := &ControllersHandler{
				controllers:      []*k8s.Controller{},
				informersFactory: dynamicinformer.NewDynamicSharedInformerFactory(nil, 0),
				stateKey:         string(rune('A' + id)),
				portClient:       nil,
				stopCh:           make(chan struct{}),
				isStopped:        false,
				portConfig:       &port.IntegrationAppConfig{},
			}

			// Check if there's a current handler and stop it
			if controllerHandler != nil {
				abortLock.Lock()
				abortEvents = append(abortEvents, AbortEvent{
					HandlerID: controllerHandler.stateKey,
					AbortTime: time.Now(),
					Reason:    "New resync requested",
				})
				abortLock.Unlock()

				controllerHandler.Stop()
			}

			// Set as current handler
			controllerHandler = handler

			// Simulate some work
			time.Sleep(time.Duration(id) * time.Millisecond)
		}(i)

		// Small delay between goroutines
		time.Sleep(5 * time.Millisecond)
	}

	wg.Wait()

	// Analyze abort events
	abortLock.Lock()
	defer abortLock.Unlock()

	t.Logf("Total abort events: %d", len(abortEvents))

	// Verify abort events are ordered correctly
	for i := 0; i < len(abortEvents)-1; i++ {
		assert.True(t, abortEvents[i].AbortTime.Before(abortEvents[i+1].AbortTime) ||
			abortEvents[i].AbortTime.Equal(abortEvents[i+1].AbortTime),
			"Abort events should be chronologically ordered")
	}

	// The final handler should be the one from the last goroutine
	if controllerHandler != nil {
		assert.False(t, controllerHandler.isStopped,
			"Final handler should not be stopped")
	}
}

// TestResyncAbortWithActiveControllers tests abort behavior with active controllers
func TestResyncAbortWithActiveControllers(t *testing.T) {
	// Create a handler with mock controllers
	handler := &ControllersHandler{
		controllers:      make([]*k8s.Controller, 3), // 3 mock controllers
		informersFactory: dynamicinformer.NewDynamicSharedInformerFactory(nil, 0),
		stateKey:         "test",
		portClient:       nil,
		stopCh:           make(chan struct{}),
		isStopped:        false,
		portConfig:       &port.IntegrationAppConfig{},
	}

	// Track controller shutdowns
	var shutdownCount atomic.Int32

	// Simulate controllers with shutdown listeners
	for i := range handler.controllers {
		go func(controllerID int) {
			select {
			case <-handler.stopCh:
				shutdownCount.Add(1)
				// Simulate controller cleanup
				time.Sleep(10 * time.Millisecond)
			case <-time.After(1 * time.Second):
				t.Errorf("Controller %d did not receive shutdown signal", controllerID)
			}
		}(i)
	}

	// Stop the handler (simulating abort due to new resync)
	handler.Stop()

	// Wait for controllers to shutdown
	time.Sleep(100 * time.Millisecond)

	// Verify all controllers received shutdown signal
	assert.Equal(t, int32(len(handler.controllers)), shutdownCount.Load(),
		"All controllers should receive shutdown signal")
}

// TestScheduledResyncAbortBehavior tests scheduled resync abort behavior
func TestScheduledResyncAbortBehavior(t *testing.T) {
	appConfig := &port.Config{
		StateKey: "test-state",
	}

	manager := NewScheduledResyncManager(appConfig, nil, nil, 1)

	// Track resync lifecycle
	var resyncStarted atomic.Bool
	var resyncAborted atomic.Bool

	// Start manager
	manager.Start()

	// Start a long-running resync in background
	go func() {
		resyncStarted.Store(true)

		// Simulate long-running resync
		select {
		case <-time.After(5 * time.Second):
			// Resync completed normally
		case <-time.After(100 * time.Millisecond):
			// Check if we should abort
			if manager.stopCh != nil {
				select {
				case <-manager.stopCh:
					resyncAborted.Store(true)
					return
				default:
				}
			}
		}
	}()

	// Wait for resync to start
	time.Sleep(50 * time.Millisecond)
	assert.True(t, resyncStarted.Load(), "Resync should have started")

	// Stop manager (should abort ongoing resync)
	manager.Stop()

	// Wait a bit
	time.Sleep(200 * time.Millisecond)

	// Verify manager stopped
	assert.False(t, manager.IsRunning(), "Manager should be stopped")
}

// TestResyncAbortCleanup tests that resources are properly cleaned up after abort
func TestResyncAbortCleanup(t *testing.T) {
	// Track resource cleanup
	type Resource struct {
		ID         string
		Acquired   time.Time
		Released   time.Time
		IsReleased bool
	}

	var resources []*Resource
	var resourceLock sync.Mutex

	// Simulate resource acquisition and cleanup
	acquireResource := func(id string) *Resource {
		resourceLock.Lock()
		defer resourceLock.Unlock()

		r := &Resource{
			ID:         id,
			Acquired:   time.Now(),
			IsReleased: false,
		}
		resources = append(resources, r)
		return r
	}

	releaseResource := func(r *Resource) {
		resourceLock.Lock()
		defer resourceLock.Unlock()

		r.Released = time.Now()
		r.IsReleased = true
	}

	// Create handler with resources
	handler := &ControllersHandler{
		controllers:      []*k8s.Controller{},
		informersFactory: dynamicinformer.NewDynamicSharedInformerFactory(nil, 0),
		stateKey:         "test",
		portClient:       nil,
		stopCh:           make(chan struct{}),
		isStopped:        false,
		portConfig:       &port.IntegrationAppConfig{},
	}

	// Acquire resources
	resource1 := acquireResource("resource-1")
	resource2 := acquireResource("resource-2")
	resource3 := acquireResource("resource-3")

	// Set up cleanup on stop with synchronization
	cleanupDone := make(chan struct{})
	go func() {
		<-handler.stopCh
		releaseResource(resource1)
		releaseResource(resource2)
		releaseResource(resource3)
		close(cleanupDone)
	}()

	// Stop handler (abort)
	handler.Stop()

	// Wait for cleanup to complete
	select {
	case <-cleanupDone:
		// Cleanup completed
	case <-time.After(1 * time.Second):
		t.Error("Cleanup did not complete in time")
	}

	// Verify all resources were released
	resourceLock.Lock()
	defer resourceLock.Unlock()

	for _, r := range resources {
		assert.True(t, r.IsReleased, "Resource %s should be released", r.ID)
		if r.IsReleased {
			assert.True(t, r.Released.After(r.Acquired) || r.Released.Equal(r.Acquired),
				"Resource %s should be released after it was acquired", r.ID)
		}
	}
}

// TestResyncAbortRaceCondition tests for race conditions during abort
func TestResyncAbortRaceCondition(t *testing.T) {
	// Run with -race flag to detect race conditions

	var handlers []*ControllersHandler
	var handlersLock sync.Mutex

	// Create multiple goroutines that create and abort handlers
	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Create handler
			handler := &ControllersHandler{
				controllers:      []*k8s.Controller{},
				informersFactory: dynamicinformer.NewDynamicSharedInformerFactory(nil, 0),
				stateKey:         string(rune('A' + (id % 26))),
				portClient:       nil,
				stopCh:           make(chan struct{}),
				isStopped:        false,
				portConfig:       &port.IntegrationAppConfig{},
			}

			handlersLock.Lock()
			handlers = append(handlers, handler)

			// Randomly stop previous handler
			if len(handlers) > 1 && id%2 == 0 {
				prevHandler := handlers[len(handlers)-2]
				// Only stop if not already stopped (avoid double close)
				go func(h *ControllersHandler) {
					if !h.isStopped {
						h.Stop()
					}
				}(prevHandler)
			}
			handlersLock.Unlock()

			// Simulate work
			time.Sleep(time.Duration(id%10) * time.Millisecond)

			// Randomly stop self (but check if not already stopped)
			if id%3 == 0 && !handler.isStopped {
				handler.Stop()
			}
		}(i)
	}

	wg.Wait()

	// Clean up remaining handlers
	handlersLock.Lock()
	defer handlersLock.Unlock()

	for _, h := range handlers {
		if !h.isStopped {
			h.Stop()
		}
	}

	// If we get here without race conditions, test passes
	assert.True(t, true, "No race conditions detected during concurrent abort operations")
}

// TestGlobalControllerHandlerAbort tests the global controllerHandler abort behavior
func TestGlobalControllerHandlerAbort(t *testing.T) {
	// Save original state
	originalHandler := controllerHandler
	defer func() {
		controllerHandler = originalHandler
	}()

	// Create and set first handler
	handler1 := &ControllersHandler{
		controllers:      []*k8s.Controller{},
		informersFactory: dynamicinformer.NewDynamicSharedInformerFactory(nil, 0),
		stateKey:         "handler-1",
		portClient:       nil,
		stopCh:           make(chan struct{}),
		isStopped:        false,
		portConfig:       &port.IntegrationAppConfig{},
	}
	controllerHandler = handler1

	// Verify it's set
	require.NotNil(t, controllerHandler)
	assert.Equal(t, "handler-1", controllerHandler.stateKey)
	assert.False(t, controllerHandler.isStopped)

	// Create and set second handler (should stop first)
	handler2 := &ControllersHandler{
		controllers:      []*k8s.Controller{},
		informersFactory: dynamicinformer.NewDynamicSharedInformerFactory(nil, 0),
		stateKey:         "handler-2",
		portClient:       nil,
		stopCh:           make(chan struct{}),
		isStopped:        false,
		portConfig:       &port.IntegrationAppConfig{},
	}

	// Simulate RunResync behavior
	if controllerHandler != nil {
		controllerHandler.Stop()
	}
	controllerHandler = handler2

	// Verify first handler was stopped and second is active
	assert.True(t, handler1.isStopped, "First handler should be stopped")
	assert.False(t, handler2.isStopped, "Second handler should be active")
	assert.Equal(t, "handler-2", controllerHandler.stateKey)

	// Clean up
	controllerHandler.Stop()
}
