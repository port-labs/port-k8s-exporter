package k8s

import (
	"testing"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/stretchr/testify/assert"
	"k8s.io/client-go/util/workqueue"
)

func newTestBatchCollector() *BatchCollector {
	return NewBatchCollector(10, 0)
}

func newTestEventItem(key string) EventItem {
	return EventItem{
		Key:         key,
		KindIndex:   0,
		ActionType:  CreateAction,
		EventSource: port.LiveEventsSource,
	}
}

func enqueueTestItem(t *testing.T, wq workqueue.RateLimitingInterface, item EventItem) {
	t.Helper()
	wq.Add(item)
	obj, shutdown := wq.Get()
	assert.False(t, shutdown)
	assert.Equal(t, item, obj)
}

func TestBatchCollectorCompletesSuccessfulWorkItems(t *testing.T) {
	bc := newTestBatchCollector()
	wq := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	eventLogger := logger.GetEventLogger("test")

	item := newTestEventItem("default/test")
	enqueueTestItem(t, wq, item)

	bc.AddEntity(port.EntityRequest{Identifier: "id-1", Blueprint: "bp"}, "kind", item)
	allSourceObjs := map[interface{}]struct{}{item: {}}

	bc.completeSuccessfulWorkItems(wq, allSourceObjs, map[interface{}]struct{}{}, map[interface{}]struct{}{}, eventLogger)

	assert.Equal(t, 0, wq.Len())
}

func TestBatchCollectorRequeuesRetryableFailuresWithoutForgetting(t *testing.T) {
	bc := newTestBatchCollector()
	wq := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	eventLogger := logger.GetEventLogger("test")

	item := newTestEventItem("default/test")
	enqueueTestItem(t, wq, item)

	failedSourceObjs := map[interface{}]struct{}{item: {}}
	bc.requeueFailedWorkItems(wq, failedSourceObjs, true, eventLogger)

	assert.Eventually(t, func() bool {
		return wq.Len() > 0
	}, time.Second, 5*time.Millisecond)

	obj, shutdown := wq.Get()
	assert.False(t, shutdown)
	assert.Equal(t, item, obj)
	assert.Greater(t, wq.NumRequeues(obj), 0)
}

func TestBatchCollectorGivesUpAfterMaxRequeues(t *testing.T) {
	bc := newTestBatchCollector()
	wq := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	eventLogger := logger.GetEventLogger("test")

	item := newTestEventItem("default/test")
	for i := 0; i <= MaxNumRequeues; i++ {
		enqueueTestItem(t, wq, item)
		failedSourceObjs := map[interface{}]struct{}{item: {}}
		bc.requeueFailedWorkItems(wq, failedSourceObjs, true, eventLogger)
	}

	assert.Equal(t, 0, wq.Len())
}

func TestBatchCollectorDoesNotRequeueWhenDisabled(t *testing.T) {
	bc := newTestBatchCollector()
	wq := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	eventLogger := logger.GetEventLogger("test")

	item := newTestEventItem("default/test")
	enqueueTestItem(t, wq, item)

	failedSourceObjs := map[interface{}]struct{}{item: {}}
	bc.requeueFailedWorkItems(wq, failedSourceObjs, false, eventLogger)

	assert.Equal(t, 0, wq.Len())
}

func TestBatchCollectorPermanentFailureTakesPrecedenceOverRetryableFailure(t *testing.T) {
	bc := newTestBatchCollector()
	wq := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	eventLogger := logger.GetEventLogger("test")

	item := newTestEventItem("default/test")
	enqueueTestItem(t, wq, item)

	failedSourceObjs := map[interface{}]struct{}{item: {}}
	permanentlyFailedSourceObjs := map[interface{}]struct{}{}
	entityWithKind := EntityWithKind{
		Entity:    port.EntityRequest{Identifier: "id-1", Blueprint: "bp"},
		Kind:      "kind",
		SourceObj: item,
	}

	bc.trackPermanentFailure(permanentlyFailedSourceObjs, failedSourceObjs, entityWithKind, "non-retryable bulk upsert error", assert.AnError, eventLogger)
	bc.completeSuccessfulWorkItems(wq, map[interface{}]struct{}{item: {}}, failedSourceObjs, permanentlyFailedSourceObjs, eventLogger)
	bc.requeueFailedWorkItems(wq, failedSourceObjs, true, eventLogger)
	bc.completePermanentFailures(wq, permanentlyFailedSourceObjs)

	assert.Equal(t, 0, wq.Len())
}
