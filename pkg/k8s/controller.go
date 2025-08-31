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
}

type TransformResult struct {
	Entities        []port.EntityRequest
	RawDataExamples []interface{}
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
				for kindIndex := range controller.Resource.KindConfigs {
					itemWithKind := item
					itemWithKind.KindIndex = kindIndex
					controller.initialSyncWorkqueue.Add(itemWithKind)
				}
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
				return
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
	entitiesSet := make(map[string]interface{})
	rawDataExamples := make([]interface{}, 0)
	shouldDeleteStaleEntities := true

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
		logger.Debug("Batch Collector has errors setting the delete flag to false")
		shouldDeleteStaleEntities = false
	}

	metrics.SetSuccessStatusConditionally(c.Resource.Kind, metrics.MetricPhaseResync, shouldDeleteStaleEntities)
	return &SyncResult{
		EntitiesSet:               entitiesSet,
		RawDataExamples:           rawDataExamples,
		ShouldDeleteStaleEntities: shouldDeleteStaleEntities,
	}
}

type EntityWithKind struct {
	Entity port.EntityRequest
	Kind   string
}

type BatchCollector struct {
	entitiesByBlueprint map[string][]EntityWithKind
	maxBatchSize        int
	timeout             time.Duration
	lastFlush           time.Time
	hasErrors           bool
}

func NewBatchCollector(maxBatchSize int, timeout time.Duration) *BatchCollector {
	return &BatchCollector{
		entitiesByBlueprint: make(map[string][]EntityWithKind),
		maxBatchSize:        maxBatchSize,
		timeout:             timeout,
		lastFlush:           time.Now(),
		hasErrors:           false,
	}
}

func (bc *BatchCollector) AddEntity(entity port.EntityRequest, kind string) {
	if bc.entitiesByBlueprint[entity.Blueprint] == nil {
		bc.entitiesByBlueprint[entity.Blueprint] = make([]EntityWithKind, 0)
	}
	bc.entitiesByBlueprint[entity.Blueprint] = append(bc.entitiesByBlueprint[entity.Blueprint], EntityWithKind{Entity: entity, Kind: kind})
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
		bc.lastFlush = time.Now()
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
	successCountWithKind := make(map[string]int)
	failedUpsertsCountWithKind := make(map[string]int)
	for _, entitiesWithKind := range bc.entitiesByBlueprint {
		totalEntities += len(entitiesWithKind)
	}
	logger.Infow("Batch processing", "totalEntities", totalEntities, "blueprintCount", len(bc.entitiesByBlueprint), "maxPayloadBytes", maxPayloadBytes, "maxEntitiesPerBlueprintBatch", maxEntitiesPerBlueprintBatch)

	for blueprint, entitiesWithKind := range bc.entitiesByBlueprint {
		if len(entitiesWithKind) == 0 {
			logger.Debugw("Skipping blueprint with no entities", "blueprint", blueprint)
			continue
		}
		entities := make([]port.EntityRequest, 0)
		entityIdToKind := make(map[string]string)
		for _, entityWithKind := range entitiesWithKind {
			entityIdToKind[fmt.Sprintf("%v", entityWithKind.Entity.Identifier)] = entityWithKind.Kind
			entities = append(entities, entityWithKind.Entity)
		}
		logger.Infow("Processing entities for blueprint", "blueprint", blueprint, "entityCount", len(entities))
		metrics.MeasureDuration(metrics.GetKindLabel(controller.Resource.Kind, nil), metrics.MetricPhaseLoad, func(phase string) (struct{}, error) {
			optimalBatchSize := calculateBulkSize(entities, maxEntitiesPerBlueprintBatch, maxPayloadBytes)
			logger.Infow("Calculated optimal batch size for blueprint", "blueprint", blueprint, "optimalBatchSize", optimalBatchSize)
			for i := 0; i < len(entities); i += optimalBatchSize {
				end := i + optimalBatchSize
				if end > len(entities) {
					end = len(entities)
				}
				batchEntities := entities[i:end]
				batchEntitiesWithKind := entitiesWithKind[i:end]
				bulkResponse, err := controller.portClient.BulkUpsertEntities(context.Background(), blueprint, batchEntities, "", controller.portClient.CreateMissingRelatedEntities)
				if err != nil {
					logger.Warnw(fmt.Sprintf("Bulk upsert failed. Blueprint: %s, Error: %s", blueprint, err.Error()), "blueprint", blueprint, "entityCount", len(batchEntities), "error", err)
					bc.fallbackToIndividualUpserts(controller, batchEntitiesWithKind, &entitiesSet, &shouldDeleteStaleEntities, &successCountWithKind, &failedUpsertsCountWithKind)
					continue
				}
				successCount := 0
				for _, result := range bulkResponse.Entities {
					successCountWithKind[entityIdToKind[result.Identifier]]++
					successCount++
					logger.Infow("Successfully upserted entity", "blueprint", blueprint, "identifier", result.Identifier)
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
					failedEntitiesWithKind := make([]EntityWithKind, 0)
					for _, entityWithKind := range batchEntitiesWithKind {
						if failedIdentifiers[fmt.Sprintf("%v", entityWithKind.Entity.Identifier)] {
							failedEntitiesWithKind = append(failedEntitiesWithKind, entityWithKind)
						}
					}
					if len(failedEntitiesWithKind) > 0 {
						bc.fallbackToIndividualUpserts(controller, failedEntitiesWithKind, &entitiesSet, &shouldDeleteStaleEntities, &successCountWithKind, &failedUpsertsCountWithKind)
					}
				}
				logger.Infow(fmt.Sprintf("Bulk upsert completed for blueprint %s.", blueprint), "blueprint", blueprint, "successCount", successCount, "failedCount", len(bulkResponse.Errors))
			}
			return struct{}{}, nil
		})
	}
	// Clear the batch
	bc.entitiesByBlueprint = make(map[string][]EntityWithKind)
	bc.lastFlush = time.Now()

	go func() {
		for kindLabel, count := range successCountWithKind {
			metrics.AddObjectCount(kindLabel, metrics.MetricLoadedResult, metrics.MetricPhaseLoad, float64(count))
		}
	}()

	go func() {
		for kindLabel, count := range failedUpsertsCountWithKind {
			metrics.AddObjectCount(kindLabel, metrics.MetricFailedResult, metrics.MetricPhaseLoad, float64(count))
		}
	}()

	return &SyncResult{
		EntitiesSet:               entitiesSet,
		RawDataExamples:           make([]interface{}, 0),
		ShouldDeleteStaleEntities: shouldDeleteStaleEntities,
	}
}

func (bc *BatchCollector) fallbackToIndividualUpserts(controller *Controller, entitiesWithKind []EntityWithKind, entitiesSet *map[string]interface{}, shouldDeleteStaleEntities *bool, successCountWithKind *map[string]int, failedUpsertsCountWithKind *map[string]int) {
	logger.Infow("Falling back to individual upserts", "entityCount", len(entitiesWithKind))

	for _, entityWithKind := range entitiesWithKind {
		handledEntity, err := controller.entityHandler(entityWithKind.Entity, CreateAction, port.ResyncSource)
		if err != nil {
			logger.Errorw("Individual upsert fallback failed", "identifier", entityWithKind.Entity.Identifier, "blueprint", entityWithKind.Entity.Blueprint, "error", err)
			(*failedUpsertsCountWithKind)[entityWithKind.Kind]++
			*shouldDeleteStaleEntities = false
		} else if handledEntity != nil {
			(*entitiesSet)[controller.portClient.GetEntityIdentifierKey(handledEntity)] = nil
			logger.Infow("Individual upsert fallback succeeded", "identifier", entityWithKind.Entity.Identifier, "blueprint", entityWithKind.Entity.Blueprint)
			(*successCountWithKind)[entityWithKind.Kind]++
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
		logger.Infow(fmt.Sprintf("Processing item %s from workqueue.", item.Key), "numRequeues", numRequeues, "controller", c.Resource.Kind, "eventSource", item.EventSource, "key", item.Key)

		k8sObj, exists, err := c.informer.GetIndexer().GetByKey(item.Key)
		if err != nil {
			logger.Errorw(fmt.Sprintf("Error fetching object %s from informer cache. Error: %s", item.Key, err.Error()), "key", item.Key, "controller", c.Resource.Kind, "error", err, "eventSource", item.EventSource)

			if numRequeues >= MaxNumRequeues {
				logger.Debugw("Removing object from workqueue because it's been requeued too many times", "error", err.Error(), "key", item.Key, "controller", c.Resource.Kind, "eventSource", item.EventSource)
				workqueue.Forget(obj)
				return nil, requeueCounterDiff, fmt.Errorf("error fetching object '%s'. giving up", item.Key)
			}

			if numRequeues == 0 {
				requeueCounterDiff = 1
			} else {
				requeueCounterDiff = 0
			}
			logger.Debugw("Requeuing object with rate limiting", "error", err.Error(), "key", item.Key, "controller", c.Resource.Kind, "eventSource", item.EventSource)
			workqueue.AddRateLimited(obj)
			return nil, requeueCounterDiff, fmt.Errorf("error fetching object '%s'. requeuing", item.Key)
		}

		if !exists {
			logger.Debugw("Object no longer exists in informer cache. removing from workqueue", "key", item.Key, "controller", c.Resource.Kind, "eventSource", item.EventSource)
			workqueue.Forget(obj)
			return nil, requeueCounterDiff, nil
		}

		rawDataExamples := make([]interface{}, 0)
		kindConfig := c.Resource.KindConfigs[item.KindIndex]
		kindLabel := metrics.GetKindLabel(c.Resource.Kind, &item.KindIndex)
		portEntities, rawDataExamplesForObj, err := c.getObjectEntities(k8sObj, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse, kindConfig.Port.ItemsToParseName, item.KindIndex)
		if err != nil {
			logger.Errorw(fmt.Sprintf("Error getting entities for object %s. Error: %s", item.Key, err.Error()), "key", item.Key, "controller", c.Resource.Kind, "error", err, "eventSource", item.EventSource)
			logger.Debugw("Marking batch collector as having errors", "controller", c.Resource.Kind)
			batchCollector.MarkError()

			if numRequeues >= MaxNumRequeues {
				logger.Debugw("Removing object from workqueue because it's been requeued too many times", "error", err.Error(), "key", item.Key, "controller", c.Resource.Kind, "eventSource", item.EventSource)
				workqueue.Forget(obj)
				metrics.AddObjectCount(kindLabel, metrics.MetricFailedResult, metrics.MetricPhaseTransform, 1)
				return nil, requeueCounterDiff, fmt.Errorf("error getting entities for object '%s'. Out of retries - object will not be processed", item.Key)
			}

			if numRequeues == 0 {
				requeueCounterDiff = 1
			} else {
				requeueCounterDiff = 0
			}
			logger.Debugw("Requeuing object with rate limiting", "error", err.Error(), "key", item.Key, "controller", c.Resource.Kind, "eventSource", item.EventSource)
			workqueue.AddRateLimited(obj)
			return nil, requeueCounterDiff, fmt.Errorf("error getting entities for object '%s'. Requeuing", item.Key)
		}

		if len(rawDataExamples) < MaxRawDataExamplesToSend {
			logger.Debugw("Adding raw data examples to batch collector", "numRawDataExamples", len(rawDataExamples), "maxRawDataExamplesToSend", MaxRawDataExamplesToSend)
			amountToAdd := min(len(rawDataExamplesForObj), MaxRawDataExamplesToSend-len(rawDataExamples))
			rawDataExamples = append(rawDataExamples, rawDataExamplesForObj[:amountToAdd]...)
		}

		for _, portEntity := range portEntities {
			logger.Debugw("Adding entity to batch collector", "identifier", portEntity.Identifier, "blueprint", portEntity.Blueprint)
			batchCollector.AddEntity(portEntity, kindLabel)
		}

		logger.Debugw("Removing object from workqueue", "key", item.Key, "controller", c.Resource.Kind, "eventSource", item.EventSource)
		workqueue.Forget(obj)
		return &SyncResult{
			EntitiesSet:               make(map[string]interface{}),
			RawDataExamples:           rawDataExamples,
			ShouldDeleteStaleEntities: true,
		}, requeueCounterDiff, nil
	}(obj)

	if err != nil {
		logger.Errorw(fmt.Sprintf("error processing next work item with batching. Error: %s", err.Error()), "error", err.Error(), "controller", c.Resource.Kind)
		utilruntime.HandleError(err)
	}

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
		logger.Infow(fmt.Sprintf("Processing item %s from workqueue.", item.Key), "numRequeues", numRequeues, "controller", c.Resource.Kind, "eventSource", item.EventSource, "key", item.Key)

		syncResult, err := c.syncHandler(item)
		if err != nil {
			logger.Errorw(fmt.Sprintf("Error syncing object %s. Error: %s", item.Key, err.Error()), "key", item.Key, "controller", c.Resource.Kind, "error", err, "eventSource", item.EventSource)

			if numRequeues >= MaxNumRequeues {
				workqueue.Forget(obj)
				return syncResult, requeueCounterDiff, fmt.Errorf("error syncing '%s' of resource '%s'. Out of retries - object will not be processed", item.Key, c.Resource.Kind)
			}

			if numRequeues == 0 {
				requeueCounterDiff = 1
			} else {
				requeueCounterDiff = 0
			}
			workqueue.AddRateLimited(obj)
			return nil, requeueCounterDiff, fmt.Errorf("error syncing '%s' of resource '%s'. Requeuing", item.Key, c.Resource.Kind)
		}

		workqueue.Forget(obj)
		return syncResult, requeueCounterDiff, nil
	}(obj)

	if err != nil {
		logger.Errorw(fmt.Sprintf("Got error while trying to sync a k8s object. Error: %s", err.Error()), "error", err.Error(), "resource", c.Resource.Kind)
		utilruntime.HandleError(err)
	}

	return syncResult, requeueCounterDiff, true
}

func (c *Controller) syncHandler(item EventItem) (*SyncResult, error) {
	obj, exists, err := c.informer.GetIndexer().GetByKey(item.Key)
	if err != nil {
		logger.Errorw(fmt.Sprintf("error fetching object with key '%s' from informer cache. Error: %s", item.Key, err.Error()), "key", item.Key, "resource", c.Resource.Kind, "error", err, "eventSource", item.EventSource)
		return nil, fmt.Errorf("error fetching object with key '%s' from informer cache: %v", item.Key, err)
	}
	if !exists {
		logger.Warnw(fmt.Sprintf("object no longer exists. Key: %s", item.Key), "key", item.Key, "resource", c.Resource.Kind, "eventSource", item.EventSource)
		utilruntime.HandleError(fmt.Errorf("'%s' in work queue no longer exists", item.Key))
		return nil, nil
	}

	return c.objectHandler(obj, item)
}

func (c *Controller) objectHandler(obj interface{}, item EventItem) (*SyncResult, error) {
	errors := make([]error, 0)
	entitiesSet := make(map[string]interface{})
	rawDataExamplesToReturn := make([]interface{}, 0)

	for kindIndex, kindConfig := range c.Resource.KindConfigs {
		logger.Debugw("Getting entities for object", "key", item.Key, "resource", c.Resource.Kind, "eventSource", item.EventSource)
		portEntities, rawDataExamples, err := c.getObjectEntities(obj, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse, kindConfig.Port.ItemsToParseName, kindIndex)
		if err != nil {
			logger.Errorw(fmt.Sprintf("error getting entities. Error: %s", err.Error()), "key", item.Key, "resource", c.Resource.Kind, "error", err, "eventSource", item.EventSource)
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
		for index, err := range errors {
			logger.Errorw(fmt.Sprintf("error handling entity for object key '%s'. Error {%d}: %s", item.Key, index, err.Error()), "key", item.Key, "error", err, "eventSource", item.EventSource)
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

func (c *Controller) getObjectEntities(obj interface{}, selector port.Selector, mappings []port.EntityMapping, itemsToParse string, itemsToParseName string, kindIndex int) ([]port.EntityRequest, []interface{}, error) {
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
		entities := make([]port.EntityRequest, 0, len(mappings))
		objectsToMap := make([]interface{}, 0)

		if itemsToParse == "" {
			logger.Debugw("No items to parse defined. adding object to objectsToMap", "object", structuredObj, "resource", c.Resource.Kind)
			objectsToMap = append(objectsToMap, structuredObj)
		} else {
			logger.Debugw("Items to parse defined. getting items by jq", "object", structuredObj, "resource", c.Resource.Kind)
			items, parseItemsError := jq.ParseArray(itemsToParse, structuredObj)
			if parseItemsError != nil {
				logger.Errorw(fmt.Sprintf("error parsing items to parse. Error: %s", parseItemsError.Error()), "object", structuredObj, "itemsToParse", itemsToParse, "error", parseItemsError, "resource", c.Resource.Kind)
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
			logger.Debugw("Checking if object passes selector", "object", objectToMap, "selector", selector.Query)
			selectorResult, err := isPassSelector(objectToMap, selector)
			logger.Debugw("Object passes selector", "object", objectToMap, "selector", selector.Query, "selectorResult", selectorResult)
			if err != nil {
				logger.Errorw(fmt.Sprintf("error checking if object passes selector. Error: %s", err.Error()), "object", objectToMap, "selector", selector.Query, "error", err)
				return &result, err
			}
			if !selectorResult {
				metrics.AddObjectCount(kindLabel, metrics.MetricFilteredOutResult, phase, 1)
				continue
			}
			logger.Debugw("Object passes selector. adding to raw data examples", "object", objectToMap, "selector", selector.Query)
			if *c.integrationConfig.SendRawDataExamples && len(rawDataExamples) < MaxRawDataExamplesToSend {
				rawDataExamples = append(rawDataExamples, objectToMap)
			}
			logger.Debugw("Mapping entities", "object", objectToMap)
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

func (c *Controller) entityHandler(portEntity port.EntityRequest, action EventActionType, eventSource port.EventSource) (*port.Entity, error) {
	switch action {
	case CreateAction, UpdateAction:
		upsertedEntity, err := c.portClient.CreateEntity(context.Background(), &portEntity, "", c.portClient.CreateMissingRelatedEntities)
		if err != nil {
			logger.Errorw(fmt.Sprintf("error upserting Port entity %s of blueprint %s. Error: %s", portEntity.Identifier, portEntity.Blueprint, err.Error()), "identifier", portEntity.Identifier, "blueprint", portEntity.Blueprint, "eventSource", eventSource, "error", err)
			return nil, fmt.Errorf("error upserting Port entity")
		}
		logger.Infow(fmt.Sprintf("Successfully upserted entity %s of blueprint %s", upsertedEntity.Identifier, upsertedEntity.Blueprint), "identifier", upsertedEntity.Identifier, "blueprint", upsertedEntity.Blueprint)
		return upsertedEntity, nil
	case DeleteAction:
		if reflect.TypeOf(portEntity.Identifier).Kind() != reflect.String {
			return nil, nil
		}

		result, err := entity.CheckIfOwnEntity(portEntity, c.portClient, eventSource)
		if err != nil {
			logger.Errorw(fmt.Sprintf("error checking if entity %s of blueprint %s is owned by this exporter. Error: %s", portEntity.Identifier, portEntity.Blueprint, err.Error()), "identifier", portEntity.Identifier, "blueprint", portEntity.Blueprint, "eventSource", eventSource, "error", err)
			return nil, fmt.Errorf("error checking if entity is owned by this exporter")
		}

		if *result {
			err := c.portClient.DeleteEntity(context.Background(), portEntity.Identifier.(string), portEntity.Blueprint, c.portClient.DeleteDependents)
			if err != nil {
				logger.Errorw(fmt.Sprintf("error deleting Port entity %s of blueprint %s. Error: %s", portEntity.Identifier, portEntity.Blueprint, err.Error()), "identifier", portEntity.Identifier, "blueprint", portEntity.Blueprint, "eventSource", eventSource, "error", err)
				return nil, fmt.Errorf("error deleting Port entity")
			}
			logger.Infow(fmt.Sprintf("Successfully deleted entity %s of blueprint %s", portEntity.Identifier, portEntity.Blueprint), "identifier", portEntity.Identifier, "blueprint", portEntity.Blueprint, "eventSource", eventSource)
		} else {
			logger.Warnw(fmt.Sprintf("trying to delete entity but didn't find it in port with this exporter ownership (statekey: %s), entity id: %s, blueprint: %s", config.ApplicationConfig.StateKey, portEntity.Identifier, portEntity.Blueprint), "identifier", portEntity.Identifier, "blueprint", portEntity.Blueprint, "eventSource", eventSource)
		}
	}

	return nil, nil
}

func (c *Controller) shouldSendUpdateEvent(old interface{}, new interface{}, updateEntityOnlyOnDiff bool) bool {

	if updateEntityOnlyOnDiff == false {
		return true
	}
	for kindIndex, kindConfig := range c.Resource.KindConfigs {
		oldEntities, _, err := c.getObjectEntities(old, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse, kindConfig.Port.ItemsToParseName, kindIndex)
		if err != nil {
			logger.Errorf("Error getting old entities: %v", err)
			return true
		}
		newEntities, _, err := c.getObjectEntities(new, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse, kindConfig.Port.ItemsToParseName, kindIndex)
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

func (c *Controller) InitialSyncWorkqueueLen() int {
	return c.initialSyncWorkqueue.Len()
}
