package k8s

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	guuid "github.com/google/uuid"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/image"
	"github.com/port-labs/port-k8s-exporter/pkg/jq"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/metrics"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/entity"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type EventActionType string

const (
	CreateAction                  EventActionType = "create"
	UpdateAction                  EventActionType = "update"
	DeleteAction                  EventActionType = "delete"
	MaxNumRequeues                int             = 4
	MaxRawDataExamplesToSend      int             = 5
	BlueprintBatchMultiplier      int             = 5
	LiveEventsWorkqueueGetTimeout                 = 5 * time.Second
)

type EventItem struct {
	Key         string
	KindIndex   int
	ActionType  EventActionType
	EventSource port.EventSource
}

type SyncResult struct {
	EntitiesSet               map[string]interface{}
	RawDataExamples           []interface{}
	ShouldDeleteStaleEntities bool
}

type Controller struct {
	Resource             port.AggregatedResource
	portClient           *cli.PortClient
	integrationConfig    *port.IntegrationAppConfig
	informer             cache.SharedIndexInformer
	lister               cache.GenericLister
	eventHandler         cache.ResourceEventHandlerRegistration
	eventsWorkqueue      workqueue.RateLimitingInterface
	initialSyncWorkqueue workqueue.RateLimitingInterface
	isInitialSyncDone    bool
	imageEnricher        *image.Enricher
}

type TransformResult struct {
	Entities        []port.EntityRequest
	RawDataExamples []interface{}
}

func (c *Controller) enqueueEventItems(workqueue workqueue.RateLimitingInterface, item EventItem) {
	for kindIndex := range c.Resource.KindConfigs {
		itemWithKind := item
		itemWithKind.KindIndex = kindIndex
		workqueue.Add(itemWithKind)
	}
}

func NewController(resource port.AggregatedResource, informer informers.GenericInformer, integrationConfig *port.IntegrationAppConfig, applicationConfig *config.ApplicationConfiguration, imageEnricher *image.Enricher) *Controller {
	// We create a new Port client for each controller because the Resty client is not thread-safe.
	portClient := cli.New(applicationConfig)

	cli.WithDeleteDependents(integrationConfig.DeleteDependents)(portClient)
	cli.WithCreateMissingRelatedEntities(integrationConfig.CreateMissingRelatedEntities)(portClient)
	controller := &Controller{
		Resource:             resource,
		portClient:           portClient,
		integrationConfig:    integrationConfig,
		informer:             informer.Informer(),
		lister:               informer.Lister(),
		initialSyncWorkqueue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		eventsWorkqueue:      workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		imageEnricher:        imageEnricher,
	}

	controller.eventHandler, _ = controller.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			logger.Debugw("got insert live event", "obj", obj)

			var err error
			var item EventItem
			item.ActionType = CreateAction
			item.Key, err = cache.MetaNamespaceKeyFunc(obj)
			if err != nil {
				logger.Errorw("Failed to get item key", "obj", obj)
				return
			}

			if controller.isInitialSyncDone || controller.eventHandler.HasSynced() {
				if !controller.isInitialSyncDone {
					logger.Debug("Setting initial sync to be done")
					controller.isInitialSyncDone = true
				}
				item.EventSource = port.LiveEventsSource
				logger.Debugw("sending the item to events queue for processing", "item", item.Key)
				controller.enqueueEventItems(controller.eventsWorkqueue, item)
			} else {
				item.EventSource = port.ResyncSource
				logger.Debugw("sending the item to resync queue for processing", "item", item.Key)
				controller.enqueueEventItems(controller.initialSyncWorkqueue, item)
			}
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			logger.Debugw("got update live event", "newObj", new, "oldObj", old)
			var err error
			var item EventItem
			item.ActionType = UpdateAction
			item.EventSource = port.LiveEventsSource
			item.Key, err = cache.MetaNamespaceKeyFunc(new)
			if err != nil {
				logger.Errorw("Failed to get item key", "newObj", new)
				return
			}

			if controller.shouldSendUpdateEvent(old, new, integrationConfig.UpdateEntityOnlyOnDiff == nil || *(integrationConfig.UpdateEntityOnlyOnDiff)) {
				logger.Debugw("sending the update event to queue for processing", "item", item.Key)
				controller.enqueueEventItems(controller.eventsWorkqueue, item)
				return
			}
			logger.Debugw("decided not to update. Ignoring.", "item", item.Key)

		},
		DeleteFunc: func(obj interface{}) {
			eventLogger := logger.GetEventLogger(guuid.NewString())
			eventLogger.Debugw("got delete live event", "obj", obj)
			var err error
			var item EventItem
			item.ActionType = DeleteAction
			item.EventSource = port.LiveEventsSource
			item.Key, err = cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				eventLogger.Errorw("Failed to get item key", "obj", obj)
				return
			}

			eventLogger.Debugw("Sending the item to object handler", "item", item.Key, "obj", obj)
			_, err = controller.objectHandler(obj, item, eventLogger)
			if err != nil {
				logger.Errorf("Error deleting item '%s' of resource '%s': %s", item.Key, resource.Kind, err.Error())
			}
		},
	})

	return controller
}

func (c *Controller) Shutdown() {
	c.initialSyncWorkqueue.ShutDown()
	c.eventsWorkqueue.ShutDown()
}

func (c *Controller) WaitForCacheSync(stopCh <-chan struct{}) error {
	if ok := cache.WaitForCacheSync(stopCh, c.informer.HasSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	return nil
}

func (c *Controller) RunInitialSync(eventLogger *zap.SugaredLogger) *SyncResult {
	entitiesSet := make(map[string]interface{})
	rawDataExamples := make([]interface{}, 0)
	shouldDeleteStaleEntities := true

	totalBatchSize := c.calculateTotalBatchSize()
	batchTimeout := time.Duration(config.ApplicationConfig.BulkSyncBatchTimeoutSeconds) * time.Second
	batchCollector := NewBatchCollector(totalBatchSize, batchTimeout)

	eventLogger.Infow("Initializing batch collector", "totalBatchSize", totalBatchSize, "batchTimeout", batchTimeout)

	poller := newWorkqueuePoller(c.initialSyncWorkqueue)
	shouldContinue := true
	requeueCounter := 0
	var requeueCounterDiff int
	var syncResult *SyncResult

	for shouldContinue && (requeueCounter > 0 || c.initialSyncWorkqueue.Len() > 0 || !c.eventHandler.HasSynced() || poller.HasPending() || batchCollector.HasPending()) {
		eventLogger.Debugw("Processing next work item with batching", "requeueCounter", requeueCounter, "initialSyncWorkqueueLen", c.initialSyncWorkqueue.Len(), "eventHandlerHasSynced", c.eventHandler.HasSynced())
		syncResult, requeueCounterDiff, shouldContinue = c.processNextWorkItemWithBatching(c.initialSyncWorkqueue, batchCollector, eventLogger, poller, 0)
		eventLogger.Debugw("Processed next work item with batching", "syncResult", syncResult, "requeueCounterDiff", requeueCounterDiff, "shouldContinue", shouldContinue)
		requeueCounter += requeueCounterDiff
		if syncResult != nil {
			entitiesSet = goutils.MergeMaps(entitiesSet, syncResult.EntitiesSet)
			amountOfExamplesToAdd := min(len(syncResult.RawDataExamples), MaxRawDataExamplesToSend-len(rawDataExamples))
			rawDataExamples = append(rawDataExamples, syncResult.RawDataExamples[:amountOfExamplesToAdd]...)
			shouldDeleteStaleEntities = shouldDeleteStaleEntities && syncResult.ShouldDeleteStaleEntities
		}
	}

	// Process any remaining batched entities without requeueing so initial sync can fail closed.
	eventLogger.Debugw("Going to process all the remaining entities in the collector.", "controller", c.Resource.Kind)
	finalSyncResult := batchCollector.ProcessRemaining(c, c.initialSyncWorkqueue, eventLogger)
	if finalSyncResult != nil {
		entitiesSet = goutils.MergeMaps(entitiesSet, finalSyncResult.EntitiesSet)
		shouldDeleteStaleEntities = shouldDeleteStaleEntities && finalSyncResult.ShouldDeleteStaleEntities
	}

	// Drain any work items requeued during batch flushes before finishing initial sync.
	for c.initialSyncWorkqueue.Len() > 0 || poller.HasPending() {
		eventLogger.Debugw("Draining requeued work items after batch flush", "initialSyncWorkqueueLen", c.initialSyncWorkqueue.Len(), "pollerHasPending", poller.HasPending())
		syncResult, requeueCounterDiff, shouldContinue = c.processNextWorkItemWithBatching(c.initialSyncWorkqueue, batchCollector, eventLogger, poller, 0)
		requeueCounter += requeueCounterDiff
		if !shouldContinue {
			break
		}
		if syncResult != nil {
			entitiesSet = goutils.MergeMaps(entitiesSet, syncResult.EntitiesSet)
			amountOfExamplesToAdd := min(len(syncResult.RawDataExamples), MaxRawDataExamplesToSend-len(rawDataExamples))
			rawDataExamples = append(rawDataExamples, syncResult.RawDataExamples[:amountOfExamplesToAdd]...)
			shouldDeleteStaleEntities = shouldDeleteStaleEntities && syncResult.ShouldDeleteStaleEntities
		}
	}

	if batchCollector.HasPending() {
		eventLogger.Debugw("Flushing remaining batched entities after draining requeued work items", "controller", c.Resource.Kind)
		remainingSyncResult := batchCollector.ProcessRemaining(c, c.initialSyncWorkqueue, eventLogger)
		if remainingSyncResult != nil {
			entitiesSet = goutils.MergeMaps(entitiesSet, remainingSyncResult.EntitiesSet)
			shouldDeleteStaleEntities = shouldDeleteStaleEntities && remainingSyncResult.ShouldDeleteStaleEntities
		}
	}

	if batchCollector.HasErrors() {
		eventLogger.Debugw("Batch Collector has errors setting the delete flag to false")
		shouldDeleteStaleEntities = false
	}

	metrics.SetSuccessStatusConditionally(c.Resource.Kind, metrics.MetricPhaseResync, shouldDeleteStaleEntities)
	return &SyncResult{
		EntitiesSet:               entitiesSet,
		RawDataExamples:           rawDataExamples,
		ShouldDeleteStaleEntities: shouldDeleteStaleEntities,
	}
}

type workqueueGetResult struct {
	obj      interface{}
	shutdown bool
}

type workqueuePoller struct {
	wq      workqueue.RateLimitingInterface
	results chan workqueueGetResult
	once    sync.Once
}

func newWorkqueuePoller(wq workqueue.RateLimitingInterface) *workqueuePoller {
	return &workqueuePoller{
		wq:      wq,
		results: make(chan workqueueGetResult, 1),
	}
}

func (p *workqueuePoller) start() {
	p.once.Do(func() {
		go func() {
			for {
				obj, shutdown := p.wq.Get()
				p.results <- workqueueGetResult{obj: obj, shutdown: shutdown}
				if shutdown {
					return
				}
			}
		}()
	})
}

func (p *workqueuePoller) HasPending() bool {
	p.start()
	return len(p.results) > 0
}

func (p *workqueuePoller) Get(timeout time.Duration) (obj interface{}, shutdown bool, ok bool) {
	p.start()
	if timeout <= 0 {
		result := <-p.results
		return result.obj, result.shutdown, true
	}
	select {
	case result := <-p.results:
		return result.obj, result.shutdown, true
	case <-time.After(timeout):
		return nil, false, false
	}
}

func (c *Controller) calculateTotalBatchSize() int {
	maxEntitiesPerBlueprintBatch := config.ApplicationConfig.BulkSyncMaxEntitiesPerBatch
	return maxEntitiesPerBlueprintBatch * BlueprintBatchMultiplier
}

func workqueueGetTimeout(batchTimeout time.Duration) time.Duration {
	getTimeout := LiveEventsWorkqueueGetTimeout
	if batchTimeout > 0 && batchTimeout < getTimeout {
		getTimeout = batchTimeout
	}
	return getTimeout
}

func (c *Controller) processNextWorkItemWithBatching(workqueue workqueue.RateLimitingInterface, batchCollector *BatchCollector, eventLogger *zap.SugaredLogger, poller *workqueuePoller, getTimeout time.Duration) (*SyncResult, int, bool) {
	if batchCollector.ShouldFlush() {
		eventLogger.Debugw("Batch collector should flush", "controller", c.Resource.Kind)
		syncResult := batchCollector.ProcessBatch(c, workqueue, eventLogger, true)
		eventLogger.Debugw("Batch collector processed batch", "numEntities", len(syncResult.EntitiesSet), "numRawDataExamples", len(syncResult.RawDataExamples), "shouldDeleteStaleEntities", syncResult.ShouldDeleteStaleEntities)
		if syncResult != nil {
			return syncResult, 0, true
		}
	}

	obj, shutdown, received := poller.Get(getTimeout)
	if !received {
		return nil, 0, true
	}
	if shutdown {
		eventLogger.Debugw("Workqueue is shutting down", "controller", c.Resource.Kind)
		return nil, 0, false
	}

	return c.processWorkqueueObjectWithBatching(workqueue, batchCollector, eventLogger, obj)
}

func (c *Controller) processWorkqueueObjectWithBatching(workqueue workqueue.RateLimitingInterface, batchCollector *BatchCollector, eventLogger *zap.SugaredLogger, obj interface{}) (*SyncResult, int, bool) {
	syncResult, requeueCounterDiff, err := func(obj interface{}) (*SyncResult, int, error) {
		type workItemCompletion int
		const (
			workItemDeferToBatch workItemCompletion = iota
			workItemComplete
			workItemRequeue
		)

		completion := workItemDeferToBatch
		defer func() {
			switch completion {
			case workItemComplete:
				workqueue.Forget(obj)
				workqueue.Done(obj)
			case workItemRequeue:
				workqueue.Done(obj)
				workqueue.AddRateLimited(obj)
			}
		}()

		numRequeues := workqueue.NumRequeues(obj)
		eventLogger.Debugw("Processing next work item in workqueue", "numRequeues", numRequeues, "controller", c.Resource.Kind)
		requeueCounterDiff := 0
		if numRequeues > 0 {
			requeueCounterDiff = -1
		}

		item, ok := obj.(EventItem)
		if !ok {
			eventLogger.Debugw("Expected event item but got something else. removing from workqueue", "obj", obj)
			completion = workItemComplete
			return nil, requeueCounterDiff, fmt.Errorf("expected event item in workqueue but got %#v", obj)
		}
		eventLogger.Infow(fmt.Sprintf("Processing item %s from workqueue.", item.Key), "numRequeues", numRequeues, "controller", c.Resource.Kind, "eventSource", item.EventSource, "key", item.Key)

		k8sObj, exists, err := c.informer.GetIndexer().GetByKey(item.Key)
		if err != nil {
			eventLogger.Errorw(fmt.Sprintf("Error fetching object %s from informer cache. Error: %s", item.Key, err.Error()), "key", item.Key, "controller", c.Resource.Kind, "error", err, "eventSource", item.EventSource)

			if numRequeues >= MaxNumRequeues {
				eventLogger.Debugw("Removing object from workqueue because it's been requeued too many times", "error", err.Error(), "key", item.Key, "controller", c.Resource.Kind, "eventSource", item.EventSource)
				completion = workItemComplete
				return nil, requeueCounterDiff, fmt.Errorf("error fetching object '%s'. giving up", item.Key)
			}

			if numRequeues == 0 {
				requeueCounterDiff = 1
			} else {
				requeueCounterDiff = 0
			}
			eventLogger.Debugw("Requeuing object with rate limiting", "error", err.Error(), "key", item.Key, "controller", c.Resource.Kind, "eventSource", item.EventSource)
			completion = workItemRequeue
			return nil, requeueCounterDiff, fmt.Errorf("error fetching object '%s'. requeuing", item.Key)
		}

		if !exists {
			eventLogger.Debugw("Object no longer exists in informer cache. removing from workqueue", "key", item.Key, "controller", c.Resource.Kind, "eventSource", item.EventSource)
			completion = workItemComplete
			return nil, requeueCounterDiff, nil
		}

		rawDataExamples := make([]interface{}, 0)
		kindConfig := c.Resource.KindConfigs[item.KindIndex]
		kindLabel := metrics.GetKindLabel(c.Resource.Kind, &item.KindIndex)
		portEntities, rawDataExamplesForObj, err := c.getObjectEntities(k8sObj, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse, kindConfig.Port.ItemsToParseName, item.KindIndex, eventLogger)
		if err != nil {
			eventLogger.Errorw(fmt.Sprintf("Error getting entities for object %s. Error: %s", item.Key, err.Error()), "key", item.Key, "controller", c.Resource.Kind, "error", err, "eventSource", item.EventSource, "kindIndex", item.KindIndex)
			eventLogger.Debugw("Marking batch collector as having errors", "controller", c.Resource.Kind)
			batchCollector.MarkError()

			if numRequeues >= MaxNumRequeues {
				eventLogger.Debugw("Removing object from workqueue because it's been requeued too many times", "error", err.Error(), "key", item.Key, "controller", c.Resource.Kind, "eventSource", item.EventSource)
				completion = workItemComplete
				metrics.AddObjectCount(kindLabel, metrics.MetricFailedResult, metrics.MetricPhaseTransform, 1)
				return nil, requeueCounterDiff, fmt.Errorf("error getting entities for object '%s'. Out of retries - object will not be processed", item.Key)
			}

			if numRequeues == 0 {
				requeueCounterDiff = 1
			} else {
				requeueCounterDiff = 0
			}
			eventLogger.Debugw("Requeuing object with rate limiting", "error", err.Error(), "key", item.Key, "controller", c.Resource.Kind, "eventSource", item.EventSource)
			completion = workItemRequeue
			return nil, requeueCounterDiff, fmt.Errorf("error getting entities for object '%s'. Requeuing", item.Key)
		}

		if len(rawDataExamples) < MaxRawDataExamplesToSend {
			eventLogger.Debugw("Adding raw data examples to batch collector", "numRawDataExamples", len(rawDataExamples), "maxRawDataExamplesToSend", MaxRawDataExamplesToSend)
			amountToAdd := min(len(rawDataExamplesForObj), MaxRawDataExamplesToSend-len(rawDataExamples))
			rawDataExamples = append(rawDataExamples, rawDataExamplesForObj[:amountToAdd]...)
		}

		if len(portEntities) == 0 {
			eventLogger.Debugw("No entities produced for object. removing from workqueue", "key", item.Key, "controller", c.Resource.Kind, "eventSource", item.EventSource, "kindIndex", item.KindIndex)
			completion = workItemComplete
			return &SyncResult{
				EntitiesSet:               make(map[string]interface{}),
				RawDataExamples:           rawDataExamples,
				ShouldDeleteStaleEntities: true,
			}, requeueCounterDiff, nil
		}

		for _, portEntity := range portEntities {
			eventLogger.Debugw("Adding entity to batch collector", "identifier", portEntity.Identifier, "blueprint", portEntity.Blueprint, "kindIndex", item.KindIndex)
			batchCollector.AddEntity(portEntity, kindLabel, obj)
		}

		eventLogger.Debugw("Deferring workqueue completion until batch flush succeeds", "key", item.Key, "controller", c.Resource.Kind, "eventSource", item.EventSource, "kindIndex", item.KindIndex)
		return &SyncResult{
			EntitiesSet:               make(map[string]interface{}),
			RawDataExamples:           rawDataExamples,
			ShouldDeleteStaleEntities: true,
		}, requeueCounterDiff, nil
	}(obj)

	if err != nil {
		eventLogger.Errorw(fmt.Sprintf("error processing next work item with batching. Error: %s", err.Error()), "error", err.Error(), "controller", c.Resource.Kind)
		utilruntime.HandleError(err)
	}

	return syncResult, requeueCounterDiff, true
}

func (c *Controller) RunEventsSync(workers int, eventLogger *zap.SugaredLogger, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash(logger.LogPanic)
	totalBatchSize := c.calculateTotalBatchSize()
	batchTimeout := time.Duration(config.ApplicationConfig.LiveEventsBulkSyncBatchTimeoutSeconds) * time.Second
	batchCollector := NewBatchCollector(totalBatchSize, batchTimeout)

	for i := 0; i < workers; i++ {
		go wait.Until(func() {
			poller := newWorkqueuePoller(c.eventsWorkqueue)
			defer batchCollector.ProcessRemaining(c, c.eventsWorkqueue, eventLogger)
			shouldContinue := true
			for shouldContinue {
				_, _, shouldContinue = c.processNextWorkItemWithBatching(c.eventsWorkqueue, batchCollector, eventLogger, poller, workqueueGetTimeout(batchTimeout))
			}
		}, time.Second, stopCh)
	}
}

func (c *Controller) syncHandler(item EventItem, eventLogger *zap.SugaredLogger) (*SyncResult, error) {
	obj, exists, err := c.informer.GetIndexer().GetByKey(item.Key)
	if err != nil {
		eventLogger.Errorw(fmt.Sprintf("error fetching object with key '%s' from informer cache. Error: %s", item.Key, err.Error()), "key", item.Key, "resource", c.Resource.Kind, "error", err, "eventSource", item.EventSource)
		return nil, fmt.Errorf("error fetching object with key '%s' from informer cache: %v", item.Key, err)
	}
	if !exists {
		eventLogger.Warnw(fmt.Sprintf("object no longer exists. Key: %s", item.Key), "key", item.Key, "resource", c.Resource.Kind, "eventSource", item.EventSource)
		utilruntime.HandleError(fmt.Errorf("'%s' in work queue no longer exists", item.Key))
		return nil, nil
	}

	return c.objectHandler(obj, item, eventLogger)
}

func (c *Controller) objectHandler(obj interface{}, item EventItem, eventLogger *zap.SugaredLogger) (*SyncResult, error) {
	errors := make([]error, 0)
	entitiesSet := make(map[string]interface{})
	rawDataExamplesToReturn := make([]interface{}, 0)

	for kindIndex, kindConfig := range c.Resource.KindConfigs {
		eventLogger.Debugw("Getting entities for object", "key", item.Key, "resource", c.Resource.Kind, "eventSource", item.EventSource)
		portEntities, rawDataExamples, err := c.getObjectEntities(obj, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse, kindConfig.Port.ItemsToParseName, kindIndex, eventLogger)
		if err != nil {
			eventLogger.Errorw(fmt.Sprintf("error getting entities. Error: %s", err.Error()), "key", item.Key, "resource", c.Resource.Kind, "error", err, "eventSource", item.EventSource)
			entitiesSet = nil
			utilruntime.HandleError(fmt.Errorf("error getting entities for object key '%s': %v", item.Key, err))
			continue
		}

		if rawDataExamplesToReturn != nil {
			amountOfExamplesToAdd := min(len(rawDataExamples), MaxRawDataExamplesToSend-len(rawDataExamplesToReturn))
			rawDataExamplesToReturn = append(rawDataExamplesToReturn, rawDataExamples[:amountOfExamplesToAdd]...)
		}

		for _, portEntity := range portEntities {
			handledEntity, err := c.entityHandler(portEntity, item.ActionType, item.EventSource, eventLogger)
			if err != nil {
				errors = append(errors, err)
				entitiesSet = nil
			}

			if entitiesSet != nil && item.ActionType != DeleteAction {
				entitiesSet[c.portClient.GetEntityIdentifierKey(handledEntity)] = nil
			}
		}
	}

	var finalErr error
	if len(errors) > 0 {
		for index, err := range errors {
			eventLogger.Errorw(fmt.Sprintf("error handling entity for object key '%s'. Error {%d}: %s", item.Key, index, err.Error()), "key", item.Key, "error", err, "eventSource", item.EventSource)
		}
		finalErr = fmt.Errorf("failed to handle entity for object key '%s'", item.Key)
	}

	return &SyncResult{
		EntitiesSet:               entitiesSet,
		RawDataExamples:           rawDataExamplesToReturn,
		ShouldDeleteStaleEntities: entitiesSet != nil,
	}, finalErr
}

func isPassSelector(obj interface{}, selector port.Selector) (bool, error) {
	if selector.Query == "" {
		return true, nil
	}

	selectorResult, err := jq.ParseBool(selector.Query, obj)
	if err != nil {
		logger.Errorw(fmt.Sprintf("invalid selector query '%s'. Error: %s", selector.Query, err.Error()), "selectorQuery", selector.Query, "error", err)
		return false, fmt.Errorf("invalid selector query")
	}

	return selectorResult, err
}

func (c *Controller) getObjectEntities(obj interface{}, selector port.Selector, mappings []port.EntityMapping, itemsToParse string, itemsToParseName string, kindIndex int, eventLogger *zap.SugaredLogger) ([]port.EntityRequest, []interface{}, error) {
	// Set default value for itemsToParseName if empty
	if itemsToParseName == "" {
		itemsToParseName = "item"
	}

	transformResult, err := metrics.MeasureDuration(metrics.GetKindLabel(c.Resource.Kind, nil), metrics.MetricPhaseTransform, func(phase string) (*TransformResult, error) {
		kindLabel := metrics.GetKindLabel(c.Resource.Kind, &kindIndex)
		var result TransformResult
		unstructuredObj, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return &result, fmt.Errorf("error casting to unstructured")
		}
		var structuredObj interface{}
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.DeepCopy().Object, &structuredObj)
		if err != nil {
			return &result, fmt.Errorf("error converting from unstructured: %v", err)
		}
		if c.imageEnricher != nil {
			if objMap, ok := structuredObj.(map[string]interface{}); ok {
				c.imageEnricher.Enrich(context.Background(), objMap)
			}
		}
		entities := make([]port.EntityRequest, 0, len(mappings))
		objectsToMap := make([]interface{}, 0)

		if itemsToParse == "" {
			eventLogger.Debugw("No items to parse defined. adding object to objectsToMap", "object", structuredObj, "resource", c.Resource.Kind)
			objectsToMap = append(objectsToMap, structuredObj)
		} else {
			eventLogger.Debugw("Items to parse defined. getting items by jq", "object", structuredObj, "resource", c.Resource.Kind)
			items, parseItemsError := jq.ParseArray(itemsToParse, structuredObj)
			if parseItemsError != nil {
				eventLogger.Errorw(fmt.Sprintf("error parsing items to parse. Error: %s", parseItemsError.Error()), "object", structuredObj, "itemsToParse", itemsToParse, "error", parseItemsError, "resource", c.Resource.Kind)
				return &result, parseItemsError
			}

			mappedObject, ok := structuredObj.(map[string]interface{})
			if !ok {
				return &result, fmt.Errorf("error parsing object '%#v'", structuredObj)
			}
			for _, item := range items {
				copiedObject := make(map[string]interface{})
				for key, value := range mappedObject {
					copiedObject[key] = value
				}
				copiedObject[itemsToParseName] = item
				objectsToMap = append(objectsToMap, copiedObject)
			}
		}
		rawDataExamples := make([]interface{}, 0)
		for _, objectToMap := range objectsToMap {
			eventLogger.Debugw("Checking if object passes selector", "object", objectToMap, "selector", selector.Query)
			selectorResult, err := isPassSelector(objectToMap, selector)
			eventLogger.Debugw("Object passes selector", "object", objectToMap, "selector", selector.Query, "selectorResult", selectorResult)
			if err != nil {
				eventLogger.Errorw(fmt.Sprintf("error checking if object passes selector. Error: %s", err.Error()), "object", objectToMap, "selector", selector.Query, "error", err)
				return &result, err
			}
			if !selectorResult {
				metrics.AddObjectCount(kindLabel, metrics.MetricFilteredOutResult, phase, 1)
				continue
			}
			eventLogger.Debugw("Object passes selector. adding to raw data examples", "object", objectToMap, "selector", selector.Query)
			if *c.integrationConfig.SendRawDataExamples && len(rawDataExamples) < MaxRawDataExamplesToSend {
				rawDataExamples = append(rawDataExamples, objectToMap)
			}
			eventLogger.Debugw("Mapping entities", "object", objectToMap)
			currentEntities, err := entity.MapEntities(objectToMap, mappings)
			if err != nil {
				return &result, err
			}
			entities = append(entities, currentEntities...)
		}
		metrics.AddObjectCount(kindLabel, metrics.MetricTransformResult, phase, float64(len(entities)))
		return &TransformResult{
			Entities:        entities,
			RawDataExamples: rawDataExamples,
		}, nil
	})
	return transformResult.Entities, transformResult.RawDataExamples, err
}

func (c *Controller) entityHandler(portEntity port.EntityRequest, action EventActionType, eventSource port.EventSource, eventLogger *zap.SugaredLogger) (*port.Entity, error) {
	switch action {
	case CreateAction, UpdateAction:
		upsertedEntity, err := c.portClient.CreateEntity(context.Background(), &portEntity, "", c.portClient.CreateMissingRelatedEntities)
		if err != nil {
			eventLogger.Errorw(fmt.Sprintf("error upserting Port entity %s of blueprint %s. Error: %s", portEntity.Identifier, portEntity.Blueprint, err.Error()), "identifier", portEntity.Identifier, "blueprint", portEntity.Blueprint, "eventSource", eventSource, "error", err)
			return nil, fmt.Errorf("error upserting Port entity")
		}
		eventLogger.Infow(fmt.Sprintf("Successfully upserted entity %s of blueprint %s", upsertedEntity.Identifier, upsertedEntity.Blueprint), "identifier", upsertedEntity.Identifier, "blueprint", upsertedEntity.Blueprint)
		return upsertedEntity, nil
	case DeleteAction:
		if reflect.TypeOf(portEntity.Identifier).Kind() != reflect.String {
			return nil, nil
		}

		result, err := entity.CheckIfOwnEntity(portEntity, c.portClient, eventSource)
		if err != nil {
			eventLogger.Errorw(fmt.Sprintf("error checking if entity %s of blueprint %s is owned by this exporter. Error: %s", portEntity.Identifier, portEntity.Blueprint, err.Error()), "identifier", portEntity.Identifier, "blueprint", portEntity.Blueprint, "eventSource", eventSource, "error", err)
			return nil, fmt.Errorf("error checking if entity is owned by this exporter")
		}

		if *result {
			err := c.portClient.DeleteEntity(context.Background(), portEntity.Identifier.(string), portEntity.Blueprint, c.portClient.DeleteDependents)
			if err != nil {
				eventLogger.Errorw(fmt.Sprintf("error deleting Port entity %s of blueprint %s. Error: %s", portEntity.Identifier, portEntity.Blueprint, err.Error()), "identifier", portEntity.Identifier, "blueprint", portEntity.Blueprint, "eventSource", eventSource, "error", err)
				return nil, fmt.Errorf("error deleting Port entity")
			}
			eventLogger.Infow(fmt.Sprintf("Successfully deleted entity %s of blueprint %s", portEntity.Identifier, portEntity.Blueprint), "identifier", portEntity.Identifier, "blueprint", portEntity.Blueprint, "eventSource", eventSource)
		} else {
			eventLogger.Warnw(fmt.Sprintf("trying to delete entity but didn't find it in port with this exporter ownership (statekey: %s), entity id: %s, blueprint: %s", config.ApplicationConfig.StateKey, portEntity.Identifier, portEntity.Blueprint), "identifier", portEntity.Identifier, "blueprint", portEntity.Blueprint, "eventSource", eventSource)
		}
	}

	return nil, nil
}

func (c *Controller) shouldSendUpdateEvent(old interface{}, new interface{}, updateEntityOnlyOnDiff bool) bool {

	if updateEntityOnlyOnDiff == false {
		return true
	}
	for kindIndex, kindConfig := range c.Resource.KindConfigs {
		oldEntities, _, err := c.getObjectEntities(old, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse, kindConfig.Port.ItemsToParseName, kindIndex, logger.GetLogger())
		if err != nil {
			logger.Errorf("Error getting old entities: %v", err)
			return true
		}
		newEntities, _, err := c.getObjectEntities(new, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse, kindConfig.Port.ItemsToParseName, kindIndex, logger.GetLogger())
		if err != nil {
			logger.Errorf("Error getting new entities: %v", err)
			return true
		}
		oldEntitiesHash, err := entity.HashAllEntities(oldEntities)
		if err != nil {
			logger.Errorf("Error hashing old entities: %v", err)
			return true
		}
		newEntitiesHash, err := entity.HashAllEntities(newEntities)
		if err != nil {
			logger.Errorf("Error hashing new entities: %v", err)
			return true
		}

		if oldEntitiesHash != newEntitiesHash {
			return true
		}
	}

	return false
}

func (c *Controller) InitialSyncWorkqueueLen() int {
	return c.initialSyncWorkqueue.Len()
}
