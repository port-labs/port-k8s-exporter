package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/port/entity"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/jq"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

type EventActionType string

const (
	CreateAction             EventActionType = "create"
	UpdateAction             EventActionType = "update"
	DeleteAction             EventActionType = "delete"
	MaxNumRequeues           int             = 4
	MaxRawDataExamplesToSend                 = 5
	DefaultMaxBulkPayloadBytes     = 1024 * 1024 // 1MiB
	DefaultMaxBulkEntitiesPerBatch = 20
	DefaultBulkBatchTimeoutSeconds = 5
	BlueprintBatchMultiplier = 5
)

type EventItem struct {
	Key        string
	ActionType EventActionType
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
			var err error
			var item EventItem
			item.ActionType = CreateAction
			item.Key, err = cache.MetaNamespaceKeyFunc(obj)
			if err != nil {
				return
			}

			if controller.isInitialSyncDone || controller.eventHandler.HasSynced() {
				if !controller.isInitialSyncDone {
					controller.isInitialSyncDone = true
				}
				controller.eventsWorkqueue.Add(item)
			} else {
				controller.initialSyncWorkqueue.Add(item)
			}
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			var err error
			var item EventItem
			item.ActionType = UpdateAction
			item.Key, err = cache.MetaNamespaceKeyFunc(new)
			if err != nil {
				return
			}

			if controller.shouldSendUpdateEvent(old, new, integrationConfig.UpdateEntityOnlyOnDiff == nil || *(integrationConfig.UpdateEntityOnlyOnDiff)) {
				controller.eventsWorkqueue.Add(item)
			}
		},
		DeleteFunc: func(obj interface{}) {
			var err error
			var item EventItem
			item.ActionType = DeleteAction
			item.Key, err = cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				return
			}

			_, err = controller.objectHandler(obj, item)
			if err != nil {
				klog.Errorf("Error deleting item '%s' of resource '%s': %s", item.Key, resource.Kind, err.Error())
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
	
	// Use a batch collector to group entities while still using the queue
	totalBatchSize := c.calculateTotalBatchSize()
	batchTimeout := c.getBulkBatchTimeout()
	batchCollector := NewBatchCollector(totalBatchSize, batchTimeout)
	
	klog.V(1).Infof("Initializing batch collector with size %d entities and timeout %v", totalBatchSize, batchTimeout)
	
	shouldContinue := true
	requeueCounter := 0
	var requeueCounterDiff int
	var syncResult *SyncResult
	
	for shouldContinue && (requeueCounter > 0 || c.initialSyncWorkqueue.Len() > 0 || !c.eventHandler.HasSynced()) {
		syncResult, requeueCounterDiff, shouldContinue = c.processNextWorkItemWithBatching(c.initialSyncWorkqueue, batchCollector)
		requeueCounter += requeueCounterDiff
		if syncResult != nil {
			entitiesSet = goutils.MergeMaps(entitiesSet, syncResult.EntitiesSet)
			amountOfExamplesToAdd := min(len(syncResult.RawDataExamples), MaxRawDataExamplesToSend-len(rawDataExamples))
			rawDataExamples = append(rawDataExamples, syncResult.RawDataExamples[:amountOfExamplesToAdd]...)
			shouldDeleteStaleEntities = shouldDeleteStaleEntities && syncResult.ShouldDeleteStaleEntities
		}
	}
	
	// Process any remaining batched entities
	finalSyncResult := batchCollector.ProcessRemaining(c)
	if finalSyncResult != nil {
		entitiesSet = goutils.MergeMaps(entitiesSet, finalSyncResult.EntitiesSet)
		shouldDeleteStaleEntities = shouldDeleteStaleEntities && finalSyncResult.ShouldDeleteStaleEntities
	}
	
	return &SyncResult{
		EntitiesSet:               entitiesSet,
		RawDataExamples:           rawDataExamples,
		ShouldDeleteStaleEntities: shouldDeleteStaleEntities,
	}
}

// BatchCollector collects entities for bulk processing
type BatchCollector struct {
	entitiesByBlueprint map[string][]port.EntityRequest
	maxBatchSize        int
	timeout             time.Duration
	lastFlush           time.Time
}

func NewBatchCollector(maxBatchSize int, timeout time.Duration) *BatchCollector {
	return &BatchCollector{
		entitiesByBlueprint: make(map[string][]port.EntityRequest),
		maxBatchSize:        maxBatchSize,
		timeout:             timeout,
		lastFlush:           time.Now(),
	}
}

func (bc *BatchCollector) AddEntity(entity port.EntityRequest) {
	if bc.entitiesByBlueprint[entity.Blueprint] == nil {
		bc.entitiesByBlueprint[entity.Blueprint] = make([]port.EntityRequest, 0)
	}
	bc.entitiesByBlueprint[entity.Blueprint] = append(bc.entitiesByBlueprint[entity.Blueprint], entity)
}

func (bc *BatchCollector) ShouldFlush() bool {
	totalEntities := 0
	for _, entities := range bc.entitiesByBlueprint {
		totalEntities += len(entities)
	}
	
	return totalEntities >= bc.maxBatchSize || time.Since(bc.lastFlush) > bc.timeout
}

func (c *Controller) getBulkMaxPayloadBytes() int {
	if c.integrationConfig.BulkSyncMaxPayloadBytes != nil {
		return *c.integrationConfig.BulkSyncMaxPayloadBytes
	}
	return DefaultMaxBulkPayloadBytes
}

func (c *Controller) getBulkMaxEntitiesPerBlueprintBatch() int {
	if c.integrationConfig.BulkSyncMaxEntitiesPerBatch != nil {
		return *c.integrationConfig.BulkSyncMaxEntitiesPerBatch
	}
	return DefaultMaxBulkEntitiesPerBatch
}

func (c *Controller) getBulkBatchTimeout() time.Duration {
	if c.integrationConfig.BulkSyncBatchTimeoutSeconds != nil {
		return time.Duration(*c.integrationConfig.BulkSyncBatchTimeoutSeconds) * time.Second
	}
	return DefaultBulkBatchTimeoutSeconds * time.Second
}

func (c *Controller) calculateTotalBatchSize() int {
	maxEntitiesPerBlueprintBatch := c.getBulkMaxEntitiesPerBlueprintBatch()
	return maxEntitiesPerBlueprintBatch * BlueprintBatchMultiplier
}

func (bc *BatchCollector) ProcessBatch(controller *Controller) *SyncResult {
	if len(bc.entitiesByBlueprint) == 0 {
		return nil
	}
	
	entitiesSet := make(map[string]interface{})
	shouldDeleteStaleEntities := true
	maxPayloadBytes := controller.getBulkMaxPayloadBytes()
	maxEntitiesPerBlueprintBatch := controller.getBulkMaxEntitiesPerBlueprintBatch()
	
	_, err := controller.portClient.Authenticate(context.Background(), controller.portClient.ClientID, controller.portClient.ClientSecret)
	if err != nil {
		klog.Errorf("error authenticating with Port: %v", err)
		return &SyncResult{
			EntitiesSet:               make(map[string]interface{}),
			RawDataExamples:           make([]interface{}, 0),
			ShouldDeleteStaleEntities: false,
		}
	}
	
	totalEntities := 0
	for _, entities := range bc.entitiesByBlueprint {
		totalEntities += len(entities)
	}
	
	klog.V(0).Infof("Batch processing %d entities across %d blueprints (limits: %d bytes, %d entities/batch)", totalEntities, len(bc.entitiesByBlueprint), maxPayloadBytes, maxEntitiesPerBlueprintBatch)
	
	for blueprint, entities := range bc.entitiesByBlueprint {
		if len(entities) == 0 {
			continue
		}
		
		klog.V(0).Infof("Processing %d entities for blueprint '%s'", len(entities), blueprint)
		
		optimalBatchSize := calculateBulkSize(entities, maxEntitiesPerBlueprintBatch, maxPayloadBytes)
		klog.V(1).Infof("Calculated optimal batch size for blueprint '%s': %d entities (based on payload size estimation)", blueprint, optimalBatchSize)
		
		for i := 0; i < len(entities); i += optimalBatchSize {
			end := i + optimalBatchSize
			if end > len(entities) {
				end = len(entities)
			}
			batchEntities := entities[i:end]
	
			bulkResponse, err := controller.portClient.BulkUpsertEntities(context.Background(), blueprint, batchEntities, "", controller.portClient.CreateMissingRelatedEntities)
			if err != nil {
				klog.Warningf("Bulk upsert failed for blueprint '%s' with %d entities, falling back to individual upserts: %v", blueprint, len(batchEntities), err)
				bc.fallbackToIndividualUpserts(controller, batchEntities, &entitiesSet, &shouldDeleteStaleEntities)
				continue
			}
			
			successCount := 0
			for _, result := range bulkResponse.Entities {
				if result.Created {
					successCount++
					klog.V(1).Infof("Successfully upserted entity '%s' of blueprint '%s'", result.Identifier, blueprint)
				}
				
				mockEntity := &port.Entity{
					Identifier: result.Identifier,
					Blueprint:  blueprint,
				}
				entitiesSet[controller.portClient.GetEntityIdentifierKey(mockEntity)] = nil
			}
			
			// Handle partial failures - retry failed entities individually
			if len(bulkResponse.Errors) > 0 {
				klog.Warningf("Bulk upsert had %d failures out of %d entities for blueprint '%s', retrying failed entities individually", len(bulkResponse.Errors), len(batchEntities), blueprint)
				
				failedIdentifiers := make(map[string]bool)
				for _, bulkError := range bulkResponse.Errors {
					failedIdentifiers[bulkError.Identifier] = true
					klog.V(1).Infof("Bulk upsert failed for entity '%s': %s", bulkError.Identifier, bulkError.Message)
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
			
			klog.V(0).Infof("Bulk upsert completed for blueprint '%s': %d successful, %d failed (retried individually)", blueprint, successCount, len(bulkResponse.Errors))
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
	klog.V(0).Infof("Falling back to individual upserts for %d entities", len(entities))
	
	for _, entity := range entities {
		handledEntity, err := controller.entityHandler(entity, CreateAction)
		if err != nil {
			klog.Errorf("Individual upsert fallback failed for entity '%v' of blueprint '%s': %v", entity.Identifier, entity.Blueprint, err)
			*shouldDeleteStaleEntities = false
		} else if handledEntity != nil {
			(*entitiesSet)[controller.portClient.GetEntityIdentifierKey(handledEntity)] = nil
			klog.V(1).Infof("Individual upsert fallback succeeded for entity '%v' of blueprint '%s'", entity.Identifier, entity.Blueprint)
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
		syncResult := batchCollector.ProcessBatch(c)
		if syncResult != nil {
			return syncResult, 0, true
		}
	}
	
	obj, shutdown := workqueue.Get()
	if shutdown {
		return nil, 0, false
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
			return nil, requeueCounterDiff, fmt.Errorf("expected event item in workqueue but got %#v", obj)
		}
		
		k8sObj, exists, err := c.informer.GetIndexer().GetByKey(item.Key)
		if err != nil {
			if numRequeues >= MaxNumRequeues {
				workqueue.Forget(obj)
				return nil, requeueCounterDiff, fmt.Errorf("error fetching object '%s': %v, giving up", item.Key, err)
			}
			
			if numRequeues == 0 {
				requeueCounterDiff = 1
			} else {
				requeueCounterDiff = 0
			}
			workqueue.AddRateLimited(obj)
			return nil, requeueCounterDiff, fmt.Errorf("error fetching object '%s': %v, requeuing", item.Key, err)
		}
		
		if !exists {
			workqueue.Forget(obj)
			return nil, requeueCounterDiff, nil
		}
		
		rawDataExamples := make([]interface{}, 0)
		for _, kindConfig := range c.Resource.KindConfigs {
			portEntities, rawDataExamplesForObj, err := c.getObjectEntities(k8sObj, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse)
			if err != nil {
				if numRequeues >= MaxNumRequeues {
					workqueue.Forget(obj)
					return nil, requeueCounterDiff, fmt.Errorf("error getting entities for object '%s': %v, giving up", item.Key, err)
				}
				
				if numRequeues == 0 {
					requeueCounterDiff = 1
				} else {
					requeueCounterDiff = 0
				}
				workqueue.AddRateLimited(obj)
				return nil, requeueCounterDiff, fmt.Errorf("error getting entities for object '%s': %v, requeuing", item.Key, err)
			}
			
			if len(rawDataExamples) < MaxRawDataExamplesToSend {
				amountToAdd := min(len(rawDataExamplesForObj), MaxRawDataExamplesToSend-len(rawDataExamples))
				rawDataExamples = append(rawDataExamples, rawDataExamplesForObj[:amountToAdd]...)
			}
			
			for _, portEntity := range portEntities {
				batchCollector.AddEntity(portEntity)
			}
		}
		
		workqueue.Forget(obj)
		return &SyncResult{
			EntitiesSet:               make(map[string]interface{}), // Will be populated when batch is processed
			RawDataExamples:           rawDataExamples,
			ShouldDeleteStaleEntities: true,
		}, requeueCounterDiff, nil
	}(obj)
	
	if err != nil {
		utilruntime.HandleError(err)
	}
	
	return syncResult, requeueCounterDiff, true
}

func (c *Controller) RunEventsSync(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()

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
		portEntities, rawDataExamples, err := c.getObjectEntities(obj, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse)
		if err != nil {
			entitiesSet = nil
			utilruntime.HandleError(fmt.Errorf("error getting entities for object key '%s': %v", item.Key, err))
			continue
		}

		if rawDataExamplesToReturn != nil {
			amountOfExamplesToAdd := min(len(rawDataExamples), MaxRawDataExamplesToSend-len(rawDataExamplesToReturn))
			rawDataExamplesToReturn = append(rawDataExamplesToReturn, rawDataExamples[:amountOfExamplesToAdd]...)
		}

		for _, portEntity := range portEntities {
			handledEntity, err := c.entityHandler(portEntity, item.ActionType)
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
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, nil, fmt.Errorf("error casting to unstructured")
	}
	var structuredObj interface{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.DeepCopy().Object, &structuredObj)
	if err != nil {
		return nil, nil, fmt.Errorf("error converting from unstructured: %v", err)
	}

	entities := make([]port.EntityRequest, 0, len(mappings))
	objectsToMap := make([]interface{}, 0)

	if itemsToParse == "" {
		objectsToMap = append(objectsToMap, structuredObj)
	} else {
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
		selectorResult, err := isPassSelector(objectToMap, selector)

		if err != nil {
			return nil, nil, err
		}

		if selectorResult {
			if *c.integrationConfig.SendRawDataExamples && len(rawDataExamples) < MaxRawDataExamplesToSend {
				rawDataExamples = append(rawDataExamples, objectToMap)
			}
			currentEntities, err := entity.MapEntities(objectToMap, mappings)
			if err != nil {
				return nil, nil, err
			}

			entities = append(entities, currentEntities...)
		}
	}

	return entities, rawDataExamples, nil
}

func (c *Controller) entityHandler(portEntity port.EntityRequest, action EventActionType) (*port.Entity, error) {
	_, err := c.portClient.Authenticate(context.Background(), c.portClient.ClientID, c.portClient.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("error authenticating with Port: %v", err)
	}

	switch action {
	case CreateAction, UpdateAction:
		upsertedEntity, err := c.portClient.CreateEntity(context.Background(), &portEntity, "", c.portClient.CreateMissingRelatedEntities)
		if err != nil {
			return nil, fmt.Errorf("error upserting Port entity '%s' of blueprint '%s': %v", portEntity.Identifier, portEntity.Blueprint, err)
		}
		klog.V(0).Infof("Successfully upserted entity '%s' of blueprint '%s'", upsertedEntity.Identifier, upsertedEntity.Blueprint)
		return upsertedEntity, nil
	case DeleteAction:
		if reflect.TypeOf(portEntity.Identifier).Kind() != reflect.String {
			return nil, nil
		}

		result, err := entity.CheckIfOwnEntity(portEntity, c.portClient)
		if err != nil {
			return nil, fmt.Errorf("error checking if entity '%s' of blueprint '%s' is owned by this exporter: %v", portEntity.Identifier, portEntity.Blueprint, err)
		}

		if *result {
			err := c.portClient.DeleteEntity(context.Background(), portEntity.Identifier.(string), portEntity.Blueprint, c.portClient.DeleteDependents)
			if err != nil {
				return nil, fmt.Errorf("error deleting Port entity '%s' of blueprint '%s': %v", portEntity.Identifier, portEntity.Blueprint, err)
			}
			klog.V(0).Infof("Successfully deleted entity '%s' of blueprint '%s'", portEntity.Identifier, portEntity.Blueprint)
		} else {
			klog.Warningf("trying to delete entity but didn't find it in port with this exporter ownership, entity id: '%s', blueprint:'%s'", portEntity.Identifier, portEntity.Blueprint)
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
			klog.Errorf("Error getting old entities: %v", err)
			return true
		}
		newEntities, _, err := c.getObjectEntities(new, kindConfig.Selector, kindConfig.Port.Entity.Mappings, kindConfig.Port.ItemsToParse)
		if err != nil {
			klog.Errorf("Error getting new entities: %v", err)
			return true
		}
		oldEntitiesHash, err := entity.HashAllEntities(oldEntities)
		if err != nil {
			klog.Errorf("Error hashing old entities: %v", err)
			return true
		}
		newEntitiesHash, err := entity.HashAllEntities(newEntities)
		if err != nil {
			klog.Errorf("Error hashing new entities: %v", err)
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
			klog.V(2).Infof("Failed to marshal entity for size calculation, using conservative estimate: %v", err)
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

