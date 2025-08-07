package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/jq"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/metrics"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"github.com/port-labs/port-k8s-exporter/pkg/port/entity"
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
	CreateAction             EventActionType = "create"
	UpdateAction             EventActionType = "update"
	DeleteAction             EventActionType = "delete"
	MaxNumRequeues           int             = 4
	MaxRawDataExamplesToSend int             = 5
	BlueprintBatchMultiplier int             = 5
)

type EventItem struct {
	Key         string
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
}

func NewController(resource port.AggregatedResource, informer informers.GenericInformer, integrationConfig *port.IntegrationAppConfig, applicationConfig *config.ApplicationConfiguration) *Controller {
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
				controller.eventsWorkqueue.Add(item)
			} else {
				item.EventSource = port.ResyncSource
				logger.Debugw("sending the item to resync queue for processing", "item", item.Key)
				controller.initialSyncWorkqueue.Add(item)
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
				controller.eventsWorkqueue.Add(item)
			}
			logger.Debugw("decided not to update. Ignoring.", "item", item.Key)

		},
		DeleteFunc: func(obj interface{}) {
			logger.Debugw("got delete live event", "obj", obj)
			var err error
			var item EventItem
			item.ActionType = DeleteAction
			item.EventSource = port.LiveEventsSource
			item.Key, err = cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				logger.Errorw("Failed to get item key", "obj", obj)
				return
			}

			logger.Debugw("Sending the item to object handler", "item", item.Key, "obj", obj)
			_, err = controller.objectHandler(obj, item)
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

func (c *Controller) RunInitialSync() *SyncResult {
	startExtract := time.Now()
	entitiesSet := make(map[string]interface{})
	rawDataExamples := make([]interface{}, 0)
	shouldDeleteStaleEntities := true
	hasError := false

	totalBatchSize := c.calculateTotalBatchSize()
	batchTimeout := time.Duration(config.ApplicationConfig.BulkSyncBatchTimeoutSeconds) * time.Second
	batchCollector := NewBatchCollector(totalBatchSize, batchTimeout)

	logger.Infow("Initializing batch collector", "totalBatchSize", totalBatchSize, "batchTimeout", batchTimeout)

	shouldContinue := true
	requeueCounter := 0
	var requeueCounterDiff int
	var syncResult *SyncResult

	for shouldContinue && (requeueCounter > 0 || c.initialSyncWorkqueue.Len() > 0 || !c.eventHandler.HasSynced()) {
		logger.Debugw("Processing next work item with batching", "requeueCounter", requeueCounter, "initialSyncWorkqueueLen", c.initialSyncWorkqueue.Len(), "eventHandlerHasSynced", c.eventHandler.HasSynced())
		syncResult, requeueCounterDiff, shouldContinue = c.processNextWorkItemWithBatching(c.initialSyncWorkqueue, batchCollector)
		if syncResult == nil && requeueCounterDiff == 0 {
			hasError = true
			metrics.ObjectCount.WithLabelValues(c.Resource.Kind, metrics.MetricFailedResult, metrics.MetricPhaseExtract).Add(1)
		}
		logger.Debugw("Processed next work item with batching", "syncResult", syncResult, "requeueCounterDiff", requeueCounterDiff, "shouldContinue", shouldContinue)
		requeueCounter += requeueCounterDiff
		if syncResult != nil {
			entitiesSet = goutils.MergeMaps(entitiesSet, syncResult.EntitiesSet)
			amountOfExamplesToAdd := min(len(syncResult.RawDataExamples), MaxRawDataExamplesToSend-len(rawDataExamples))
			rawDataExamples = append(rawDataExamples, syncResult.RawDataExamples[:amountOfExamplesToAdd]...)
			shouldDeleteStaleEntities = shouldDeleteStaleEntities && syncResult.ShouldDeleteStaleEntities
		}
	}

	// Process any remaining batched entities
	logger.Debugw("Going to process all the remaining entities in the collector.", "controller", c.Resource.Kind)
	finalSyncResult := batchCollector.ProcessRemaining(c)
	if finalSyncResult != nil {
		entitiesSet = goutils.MergeMaps(entitiesSet, finalSyncResult.EntitiesSet)
		shouldDeleteStaleEntities = shouldDeleteStaleEntities && finalSyncResult.ShouldDeleteStaleEntities
	}

	if batchCollector.HasErrors() {
		hasError = true
	}

	// Extraction phase ends after all work items processed
	durationExtract := time.Since(startExtract).Seconds()
	metrics.DurationSeconds.WithLabelValues(c.Resource.Kind, metrics.MetricPhaseExtract).Set(durationExtract)
	metrics.ObjectCount.WithLabelValues(c.Resource.Kind, metrics.MetricRawExtractedResult, metrics.MetricPhaseExtract).Set(float64(len(entitiesSet)))

	if hasError {
		metrics.Success.WithLabelValues(c.Resource.Kind, metrics.MetricPhaseExtract).Set(0)
	} else {
		metrics.Success.WithLabelValues(c.Resource.Kind, metrics.MetricPhaseExtract).Set(1)
	}

	return &SyncResult{
		EntitiesSet:               entitiesSet,
		RawDataExamples:           rawDataExamples,
		ShouldDeleteStaleEntities: shouldDeleteStaleEntities,
	}
}

type BatchCollector struct {
	entitiesByBlueprint map[string][]port.EntityRequest
	maxBatchSize        int
	timeout             time.Duration
	lastFlush           time.Time
	hasErrors           bool
}

func NewBatchCollector(maxBatchSize int, timeout time.Duration) *BatchCollector {
	return &BatchCollector{
		entitiesByBlueprint: make(map[string][]port.EntityRequest),
		maxBatchSize:        maxBatchSize,
		timeout:             timeout,
		lastFlush:           time.Now(),
		hasErrors:           false,
	}
}

func (bc *BatchCollector) AddEntity(entity port.EntityRequest) {
	if bc.entitiesByBlueprint[entity.Blueprint] == nil {
		bc.entitiesByBlueprint[entity.Blueprint] = make([]port.EntityRequest, 0)
	}
	bc.entitiesByBlueprint[entity.Blueprint] = append(bc.entitiesByBlueprint[entity.Blueprint], entity)
}

func (bc *BatchCollector) MarkError() {
	bc.hasErrors = true
}

func (bc *BatchCollector) HasErrors() bool {
	return bc.hasErrors
}

func (bc *BatchCollector) ShouldFlush() bool {
	totalEntities := 0
	for _, entities := range bc.entitiesByBlueprint {
		totalEntities += len(entities)
	}

	return totalEntities >= bc.maxBatchSize || time.Since(bc.lastFlush) > bc.timeout
}

func (c *Controller) calculateTotalBatchSize() int {
	maxEntitiesPerBlueprintBatch := config.ApplicationConfig.BulkSyncMaxEntitiesPerBatch
	return maxEntitiesPerBlueprintBatch * BlueprintBatchMultiplier
}

func (bc *BatchCollector) ProcessBatch(controller *Controller) *SyncResult {
	if len(bc.entitiesByBlueprint) == 0 {
		logger.Debugw("Batch collector has no entities to process", "hasErrors", bc.hasErrors, "controller", controller.Resource.Kind)
		return &SyncResult{
			EntitiesSet:               make(map[string]interface{}),
			RawDataExamples:           make([]interface{}, 0),
			ShouldDeleteStaleEntities: !bc.hasErrors,
		}
	}

	entitiesSet := make(map[string]interface{})
	shouldDeleteStaleEntities := !bc.hasErrors
	maxPayloadBytes := config.ApplicationConfig.BulkSyncMaxPayloadBytes
	maxEntitiesPerBlueprintBatch := config.ApplicationConfig.BulkSyncMaxEntitiesPerBatch

	totalEntities := 0
	for _, entities := range bc.entitiesByBlueprint {
		totalEntities += len(entities)
	}
	logger.Infow("Batch processing", "totalEntities", totalEntities, "blueprintCount", len(bc.entitiesByBlueprint), "maxPayloadBytes", maxPayloadBytes, "maxEntitiesPerBlueprintBatch", maxEntitiesPerBlueprintBatch)

	for blueprint, entities := range bc.entitiesByBlueprint {
		if len(entities) == 0 {
			logger.Debugw("Skipping blueprint with no entities", "blueprint", blueprint)
			continue
		}

		logger.Infow("Processing entities for blueprint", "blueprint", blueprint, "entityCount", len(entities))

		optimalBatchSize := calculateBulkSize(entities, maxEntitiesPerBlueprintBatch, maxPayloadBytes)
		logger.Infow("Calculated optimal batch size for blueprint", "blueprint", blueprint, "optimalBatchSize", optimalBatchSize)

		for i := 0; i < len(entities); i += optimalBatchSize {
			end := i + optimalBatchSize
			if end > len(entities) {
				end = len(entities)
			}
			batchEntities := entities[i:end]

			bulkResponse, err := controller.portClient.BulkUpsertEntities(context.Background(), blueprint, batchEntities, "", controller.portClient.CreateMissingRelatedEntities)
			if err != nil {
				logger.Warnw("Bulk upsert failed", "blueprint", blueprint, "entityCount", len(batchEntities), "error", err)
				bc.fallbackToIndividualUpserts(controller, batchEntities, &entitiesSet, &shouldDeleteStaleEntities)
				continue
			}

			successCount := 0
			for _, result := range bulkResponse.Entities {
				if result.Created {
					successCount++
					logger.Infow("Successfully upserted entity", "blueprint", blueprint, "identifier", result.Identifier)
				}

				mockEntity := &port.Entity{
					Identifier: result.Identifier,
					Blueprint:  blueprint,
				}
				entitiesSet[controller.portClient.GetEntityIdentifierKey(mockEntity)] = nil
			}

			// Handle partial failures - retry failed entities individually
			if len(bulkResponse.Errors) > 0 {
				logger.Warnw("Bulk upsert had failures", "blueprint", blueprint, "failedCount", len(bulkResponse.Errors), "totalCount", len(batchEntities))

				failedIdentifiers := make(map[string]bool)
				for _, bulkError := range bulkResponse.Errors {
					failedIdentifiers[bulkError.Identifier] = true
					logger.Infow("Bulk upsert failed for entity", "blueprint", blueprint, "identifier", bulkError.Identifier, "message", bulkError.Message)
				}

				failedEntities := make([]port.EntityRequest, 0)
				for _, entity := range batchEntities {
					if failedIdentifiers[fmt.Sprintf("%v", entity.Identifier)] {
						failedEntities = append(failedEntities, entity)
					}
				}

				if len(failedEntities) > 0 {
					bc.fallbackToIndividualUpserts(controller, failedEntities, &entitiesSet, &shouldDeleteStaleEntities)
				}
			}

			logger.Infow("Bulk upsert completed for blueprint", "blueprint", blueprint, "successCount", successCount, "failedCount", len(bulkResponse.Errors))
		}
	}

	// Clear the batch
	bc.entitiesByBlueprint = make(map[string][]port.EntityRequest)
	bc.lastFlush = time.Now()

	return &SyncResult{
		EntitiesSet:               entitiesSet,
		RawDataExamples:           make([]interface{}, 0),
		ShouldDeleteStaleEntities: shouldDeleteStaleEntities,
	}
}

func (bc *BatchCollector) fallbackToIndividualUpserts(controller *Controller, entities []port.EntityRequest, entitiesSet *map[string]interface{}, shouldDeleteStaleEntities *bool) {
	logger.Infow("Falling back to individual upserts", "entityCount", len(entities))

	for _, entity := range entities {
		handledEntity, err := controller.entityHandler(entity, CreateAction, port.ResyncSource)
		if err != nil {
			logger.Errorw("Individual upsert fallback failed", "identifier", entity.Identifier, "blueprint", entity.Blueprint, "error", err)
			*shouldDeleteStaleEntities = false
		} else if handledEntity != nil {
			(*entitiesSet)[controller.portClient.GetEntityIdentifierKey(handledEntity)] = nil
			logger.Infow("Individual upsert fallback succeeded", "identifier", entity.Identifier, "blueprint", entity.Blueprint)
		}
	}
}

func (bc *BatchCollector) ProcessRemaining(controller *Controller) *SyncResult {
	if len(bc.entitiesByBlueprint) == 0 {
		return nil
	}
	return bc.ProcessBatch(controller)
}

func (c *Controller) processNextWorkItemWithBatching(workqueue workqueue.RateLimitingInterface, batchCollector *BatchCollector) (*SyncResult, int, bool) {
	startLoad := time.Now()
	if batchCollector.ShouldFlush() {
		logger.Debugw("Batch collector should flush", "controller", c.Resource.Kind)
		syncResult := batchCollector.ProcessBatch(c)
		logger.Debugw("Batch collector processed batch", "numEntities", len(syncResult.EntitiesSet), "numRawDataExamples", len(syncResult.RawDataExamples), "shouldDeleteStaleEntities", syncResult.ShouldDeleteStaleEntities)
		if syncResult != nil {
			return syncResult, 0, true
		}
	}

	obj, shutdown := workqueue.Get()
	if shutdown {
		logger.Debugw("Workqueue is shutting down", "controller", c.Resource.Kind)
		return nil, 0, false
	}

	syncResult, requeueCounterDiff, err := func(obj interface{}) (*SyncResult, int, error) {
		defer workqueue.Done(obj)

		numRequeues := workqueue.NumRequeues(obj)
		logger.Debugw("Processing next work item in workqueue", "numRequeues", numRequeues, "controller", c.Resource.Kind)
		requeueCounterDiff := 0
		if numRequeues > 0 {
			requeueCounterDiff = -1
		}

		item, ok := obj.(EventItem)
		if !ok {
			logger.Debugw("Expected event item but got something else. removing from workqueue", "obj", obj)
			workqueue.Forget(obj)
			return nil, requeueCounterDiff, fmt.Errorf("expected event item in workqueue but got %#v", obj)
		}

		k8sObj, exists, err := c.informer.GetIndexer().GetByKey(item.Key)
		if err != nil {
			if numRequeues >= MaxNumRequeues {
				logger.Debugw("Removing object from workqueue because it's been requeued too many times", "error", err.Error(), "key", item.Key, "controller", c.Resource.Kind)
				workqueue.Forget(obj)
				return nil, requeueCounterDiff, fmt.Errorf("error fetching object '%s': %v, giving up", item.Key, err)
			}

			if numRequeues == 0 {
				requeueCounterDiff = 1
			} else {
				requeueCounterDiff = 0
			}
			logger.Debugw("Requeuing object with rate limiting", "error", err.Error(), "key", item.Key, "controller", c.Resource.Kind)
			workqueue.AddRateLimited(obj)
			return nil, requeueCounterDiff, fmt.Errorf("error fetching object '%s': %v, requeuing", item.Key, err)
		}

		if !exists {
			logger.Debugw("Object no longer exists in informer cache. removing from workqueue", "key", item.Key, "controller", c.Resource.Kind)
			workqueue.Forget(obj)
			return nil, requeueCounterDiff, nil
		}

		rawDataExamples := make([]interface{}, 0)
		for _, kindConfig := range c.Resource.KindConfigs {
			portEntities, rawDataExamplesForObj, err := c.getObjectEntities(k8sObj, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse)
			if err != nil {
				logger.Debugw("Error getting entities for object. marking batch collector as having errors", "error", err.Error(), "key", item.Key, "controller", c.Resource.Kind)
				batchCollector.MarkError()

				if numRequeues >= MaxNumRequeues {
					logger.Debugw("Removing object from workqueue because it's been requeued too many times", "error", err.Error(), "key", item.Key, "controller", c.Resource.Kind)
					workqueue.Forget(obj)
					return nil, requeueCounterDiff, fmt.Errorf("error getting entities for object '%s': %v, giving up", item.Key, err)
				}

				if numRequeues == 0 {
					requeueCounterDiff = 1
				} else {
					requeueCounterDiff = 0
				}
				logger.Debugw("Requeuing object with rate limiting", "error", err.Error(), "key", item.Key, "controller", c.Resource.Kind)
				workqueue.AddRateLimited(obj)
				return nil, requeueCounterDiff, fmt.Errorf("error getting entities for object '%s': %v, requeuing", item.Key, err)
			}

			if len(rawDataExamples) < MaxRawDataExamplesToSend {
				logger.Debugw("Adding raw data examples to batch collector", "numRawDataExamples", len(rawDataExamples), "maxRawDataExamplesToSend", MaxRawDataExamplesToSend)
				amountToAdd := min(len(rawDataExamplesForObj), MaxRawDataExamplesToSend-len(rawDataExamples))
				rawDataExamples = append(rawDataExamples, rawDataExamplesForObj[:amountToAdd]...)
			}

			for _, portEntity := range portEntities {
				logger.Debugw("Adding entity to batch collector", "identifier", portEntity.Identifier, "blueprint", portEntity.Blueprint)
				batchCollector.AddEntity(portEntity)
			}
		}

		logger.Debugw("Forgetting object from workqueue", "key", item.Key, "controller", c.Resource.Kind)
		workqueue.Forget(obj)
		return &SyncResult{
			EntitiesSet:               make(map[string]interface{}),
			RawDataExamples:           rawDataExamples,
			ShouldDeleteStaleEntities: true,
		}, requeueCounterDiff, nil
	}(obj)

	if err != nil {
		logger.Errorw("error processing next work item with batching", "error", err.Error(), "controller", c.Resource.Kind)
		utilruntime.HandleError(err)
	}

	durationLoad := time.Since(startLoad).Seconds()
	metrics.DurationSeconds.WithLabelValues(c.Resource.Kind, metrics.MetricPhaseLoad).Set(durationLoad)
	return syncResult, requeueCounterDiff, true
}

func (c *Controller) RunEventsSync(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash(logger.LogPanic)

	for i := 0; i < workers; i++ {
		go wait.Until(func() {
			shouldContinue := true
			for shouldContinue {
				_, _, shouldContinue = c.processNextWorkItem(c.eventsWorkqueue)
			}
		}, time.Second, stopCh)
	}
}

func (c *Controller) processNextWorkItem(workqueue workqueue.RateLimitingInterface) (*SyncResult, int, bool) {
	permanentErrorSyncResult := &SyncResult{
		EntitiesSet:               make(map[string]interface{}),
		RawDataExamples:           make([]interface{}, 0),
		ShouldDeleteStaleEntities: false,
	}

	obj, shutdown := workqueue.Get()

	if shutdown {
		return permanentErrorSyncResult, 0, false
	}

	syncResult, requeueCounterDiff, err := func(obj interface{}) (*SyncResult, int, error) {
		defer workqueue.Done(obj)

		numRequeues := workqueue.NumRequeues(obj)
		requeueCounterDiff := 0
		if numRequeues > 0 {
			requeueCounterDiff = -1
		}

		item, ok := obj.(EventItem)

		if !ok {
			workqueue.Forget(obj)
			return permanentErrorSyncResult, requeueCounterDiff, fmt.Errorf("expected event item of resource '%s' in workqueue but got %#v", c.Resource.Kind, obj)
		}

		syncResult, err := c.syncHandler(item)
		if err != nil {
			if numRequeues >= MaxNumRequeues {
				workqueue.Forget(obj)
				return syncResult, requeueCounterDiff, fmt.Errorf("error syncing '%s' of resource '%s': %s, give up after %d requeues", item.Key, c.Resource.Kind, err.Error(), MaxNumRequeues)
			}

			if numRequeues == 0 {
				requeueCounterDiff = 1
			} else {
				requeueCounterDiff = 0
			}
			workqueue.AddRateLimited(obj)
			return nil, requeueCounterDiff, fmt.Errorf("error syncing '%s' of resource '%s': %s, requeuing", item.Key, c.Resource.Kind, err.Error())
		}

		workqueue.Forget(obj)
		return syncResult, requeueCounterDiff, nil
	}(obj)

	if err != nil {
		logger.Errorw("error syncing", "error", err.Error(), "resource", c.Resource.Kind)
		utilruntime.HandleError(err)
	}

	return syncResult, requeueCounterDiff, true
}

func (c *Controller) syncHandler(item EventItem) (*SyncResult, error) {
	obj, exists, err := c.informer.GetIndexer().GetByKey(item.Key)
	if err != nil {
		return nil, fmt.Errorf("error fetching object with key '%s' from informer cache: %v", item.Key, err)
	}
	if !exists {
		logger.Errorw("object no longer exists", "key", item.Key, "resource", c.Resource.Kind)
		utilruntime.HandleError(fmt.Errorf("'%s' in work queue no longer exists", item.Key))
		return nil, nil
	}

	return c.objectHandler(obj, item)
}

func (c *Controller) objectHandler(obj interface{}, item EventItem) (*SyncResult, error) {
	errors := make([]error, 0)
	entitiesSet := make(map[string]interface{})
	rawDataExamplesToReturn := make([]interface{}, 0)

	for _, kindConfig := range c.Resource.KindConfigs {
		logger.Debugw("Getting entities for object", "key", item.Key, "resource", c.Resource.Kind)
		portEntities, rawDataExamples, err := c.getObjectEntities(obj, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse)
		if err != nil {
			logger.Errorw("error getting entities", "error", err.Error(), "key", item.Key, "resource", c.Resource.Kind)
			entitiesSet = nil
			utilruntime.HandleError(fmt.Errorf("error getting entities for object key '%s': %v", item.Key, err))
			continue
		}

		if rawDataExamplesToReturn != nil {
			amountOfExamplesToAdd := min(len(rawDataExamples), MaxRawDataExamplesToSend-len(rawDataExamplesToReturn))
			rawDataExamplesToReturn = append(rawDataExamplesToReturn, rawDataExamples[:amountOfExamplesToAdd]...)
		}

		for _, portEntity := range portEntities {
			handledEntity, err := c.entityHandler(portEntity, item.ActionType, item.EventSource)
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
		finalErr = fmt.Errorf("error handling entity for object key '%s': %v", item.Key, errors)
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
		return false, fmt.Errorf("invalid selector query '%s': %v", selector.Query, err)
	}

	return selectorResult, err
}

func (c *Controller) getObjectEntities(obj interface{}, selector port.Selector, mappings []port.EntityMapping, itemsToParse string) ([]port.EntityRequest, []interface{}, error) {
	startTransform := time.Now()
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		metrics.ObjectCount.WithLabelValues(c.Resource.Kind, metrics.MetricFailedResult, metrics.MetricPhaseTransform).Add(1)
		return nil, nil, fmt.Errorf("error casting to unstructured")
	}
	var structuredObj interface{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.DeepCopy().Object, &structuredObj)
	if err != nil {
		metrics.ObjectCount.WithLabelValues(c.Resource.Kind, metrics.MetricFailedResult, metrics.MetricPhaseTransform).Add(1)
		return nil, nil, fmt.Errorf("error converting from unstructured: %v", err)
	}

	entities := make([]port.EntityRequest, 0, len(mappings))
	objectsToMap := make([]interface{}, 0)

	if itemsToParse == "" {
		logger.Debugw("No items to parse defined. adding object to objectsToMap", "object", structuredObj, "resource", c.Resource.Kind)
		objectsToMap = append(objectsToMap, structuredObj)
	} else {
		logger.Debugw("Items to parse defined. getting items by jq", "object", structuredObj, "resource", c.Resource.Kind)
		items, parseItemsError := jq.ParseArray(itemsToParse, structuredObj)
		if parseItemsError != nil {
			return nil, nil, parseItemsError
		}

		mappedObject, ok := structuredObj.(map[string]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("error parsing object '%#v'", structuredObj)
		}

		for _, item := range items {
			copiedObject := make(map[string]interface{})
			for key, value := range mappedObject {
				copiedObject[key] = value
			}
			copiedObject["item"] = item
			objectsToMap = append(objectsToMap, copiedObject)
		}
	}

	rawDataExamples := make([]interface{}, 0)
	for _, objectToMap := range objectsToMap {
		logger.Debugw("Checking if object passes selector", "object", objectToMap, "selector", selector.Query)
		selectorResult, err := isPassSelector(objectToMap, selector)
		logger.Debugw("Object passes selector", "object", objectToMap, "selector", selector.Query, "selectorResult", selectorResult)
		if err != nil {
			metrics.ObjectCount.WithLabelValues(c.Resource.Kind, metrics.MetricFailedResult, metrics.MetricPhaseTransform).Add(1)
			return nil, nil, err
		}
		if !selectorResult {
			metrics.ObjectCount.WithLabelValues(c.Resource.Kind, metrics.MetricFilteredOutResult, metrics.MetricPhaseTransform).Add(1)
			continue
		}
		logger.Debugw("Object passes selector. adding to raw data examples", "object", objectToMap, "selector", selector.Query)
		if *c.integrationConfig.SendRawDataExamples && len(rawDataExamples) < MaxRawDataExamplesToSend {
			rawDataExamples = append(rawDataExamples, objectToMap)
		}
		logger.Debugw("Mapping entities", "object", objectToMap)
		currentEntities, err := entity.MapEntities(objectToMap, mappings)
		if err != nil {
			metrics.ObjectCount.WithLabelValues(c.Resource.Kind, metrics.MetricFailedResult, metrics.MetricPhaseTransform).Add(1)
			return nil, nil, err
		}
		entities = append(entities, currentEntities...)
	}
	durationTransform := time.Since(startTransform).Seconds()
	metrics.DurationSeconds.WithLabelValues(c.Resource.Kind, metrics.MetricPhaseTransform).Add(durationTransform)
	metrics.ObjectCount.WithLabelValues(c.Resource.Kind, metrics.MetricTransformResult, metrics.MetricPhaseTransform).Add(float64(len(entities)))
	return entities, rawDataExamples, nil
}

func (c *Controller) entityHandler(portEntity port.EntityRequest, action EventActionType, eventSource port.EventSource) (*port.Entity, error) {
	switch action {
	case CreateAction, UpdateAction:
		upsertedEntity, err := c.portClient.CreateEntity(context.Background(), &portEntity, "", c.portClient.CreateMissingRelatedEntities)
		if err != nil {
			return nil, fmt.Errorf("error upserting Port entity '%s' of blueprint '%s': %v", portEntity.Identifier, portEntity.Blueprint, err)
		}
		logger.Infof("Successfully upserted entity '%s' of blueprint '%s'", upsertedEntity.Identifier, upsertedEntity.Blueprint)
		return upsertedEntity, nil
	case DeleteAction:
		if reflect.TypeOf(portEntity.Identifier).Kind() != reflect.String {
			return nil, nil
		}

		result, err := entity.CheckIfOwnEntity(portEntity, c.portClient, eventSource)
		if err != nil {
			return nil, fmt.Errorf("error checking if entity '%s' of blueprint '%s' is owned by this exporter: %v", portEntity.Identifier, portEntity.Blueprint, err)
		}

		if *result {
			err := c.portClient.DeleteEntity(context.Background(), portEntity.Identifier.(string), portEntity.Blueprint, c.portClient.DeleteDependents)
			if err != nil {
				return nil, fmt.Errorf("error deleting Port entity '%s' of blueprint '%s': %v", portEntity.Identifier, portEntity.Blueprint, err)
			}
			logger.Infof("Successfully deleted entity '%s' of blueprint '%s'", portEntity.Identifier, portEntity.Blueprint)
		} else {
			logger.Warningf("trying to delete entity but didn't find it in port with this exporter ownership, entity id: '%s', blueprint:'%s'", portEntity.Identifier, portEntity.Blueprint)
		}
	}

	return nil, nil
}

func (c *Controller) shouldSendUpdateEvent(old interface{}, new interface{}, updateEntityOnlyOnDiff bool) bool {

	if updateEntityOnlyOnDiff == false {
		return true
	}
	for _, kindConfig := range c.Resource.KindConfigs {
		oldEntities, _, err := c.getObjectEntities(old, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse)
		if err != nil {
			logger.Errorf("Error getting old entities: %v", err)
			return true
		}
		newEntities, _, err := c.getObjectEntities(new, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse)
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

// calculateBulkSize determines the optimal batch size based on entity size estimation
func calculateBulkSize(entities []port.EntityRequest, maxLength int, maxSizeInBytes int) int {
	if len(entities) == 0 {
		return 1
	}

	// Calculate average object size from a sample
	sampleSize := int(math.Min(10, float64(len(entities))))
	sampleEntities := entities[:sampleSize]

	totalSampleSize := 0
	for _, entity := range sampleEntities {
		entityBytes, err := json.Marshal(entity)
		if err != nil {
			logger.Infow("Failed to marshal entity for size calculation, using conservative estimate", "error", err)
			totalSampleSize += 1024 // 1KB conservative estimate per entity
			continue
		}
		totalSampleSize += len(entityBytes)
	}

	averageObjectSize := float64(totalSampleSize) / float64(sampleSize)

	// Use a conservative estimate (1.5x the average) to ensure we stay under the limit
	estimatedObjectSize := int(math.Ceil(averageObjectSize * 1.5))
	maxObjectsPerBatch := int(math.Min(float64(maxLength), math.Floor(float64(maxSizeInBytes)/float64(estimatedObjectSize))))

	return int(math.Max(1, float64(maxObjectsPerBatch)))
}
