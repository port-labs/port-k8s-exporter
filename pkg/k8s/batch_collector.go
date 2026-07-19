package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/metrics"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/port-labs/port-k8s-exporter/pkg/port/cli"
	"go.uber.org/zap"
	"k8s.io/client-go/util/workqueue"
)

type EntityWithKind struct {
	Entity    port.EntityRequest
	Kind      string
	SourceObj interface{}
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

func (bc *BatchCollector) AddEntity(entity port.EntityRequest, kind string, sourceObj interface{}) {
	if bc.entitiesByBlueprint[entity.Blueprint] == nil {
		bc.entitiesByBlueprint[entity.Blueprint] = make([]EntityWithKind, 0)
	}
	bc.entitiesByBlueprint[entity.Blueprint] = append(bc.entitiesByBlueprint[entity.Blueprint], EntityWithKind{Entity: entity, Kind: kind, SourceObj: sourceObj})
}

func (bc *BatchCollector) HasPending() bool {
	for _, entities := range bc.entitiesByBlueprint {
		if len(entities) > 0 {
			return true
		}
	}
	return false
}

func (bc *BatchCollector) collectSourceObjs(sourceObjs map[interface{}]struct{}, entitiesWithKind []EntityWithKind) {
	for _, entityWithKind := range entitiesWithKind {
		if entityWithKind.SourceObj != nil {
			sourceObjs[entityWithKind.SourceObj] = struct{}{}
		}
	}
}

func (bc *BatchCollector) trackFailedWorkItem(failedSourceObjs map[interface{}]struct{}, sourceObj interface{}) {
	if sourceObj != nil {
		failedSourceObjs[sourceObj] = struct{}{}
	}
}

func eventItemFromWorkqueueObj(obj interface{}) (EventItem, bool) {
	item, ok := obj.(EventItem)
	return item, ok
}

func (bc *BatchCollector) logPermanentPortWriteFailure(entityWithKind EntityWithKind, reason string, err error, eventLogger *zap.SugaredLogger) {
	logFields := []interface{}{
		"reason", reason,
		"identifier", entityWithKind.Entity.Identifier,
		"blueprint", entityWithKind.Entity.Blueprint,
		"kind", entityWithKind.Kind,
	}
	if err != nil {
		logFields = append(logFields, "error", err)
	}
	if item, ok := eventItemFromWorkqueueObj(entityWithKind.SourceObj); ok {
		logFields = append(logFields, "key", item.Key, "kindIndex", item.KindIndex, "actionType", item.ActionType, "eventSource", item.EventSource)
	}
	eventLogger.Warnw("Dropping work item after non-retryable Port write failure", logFields...)
}

func (bc *BatchCollector) trackPermanentFailure(permanentlyFailedSourceObjs map[interface{}]struct{}, failedSourceObjs map[interface{}]struct{}, entityWithKind EntityWithKind, reason string, err error, eventLogger *zap.SugaredLogger) {
	bc.logPermanentPortWriteFailure(entityWithKind, reason, err, eventLogger)
	bc.trackFailedWorkItem(permanentlyFailedSourceObjs, entityWithKind.SourceObj)
	delete(failedSourceObjs, entityWithKind.SourceObj)
}

func (bc *BatchCollector) completeWorkItem(workqueue workqueue.RateLimitingInterface, sourceObj interface{}) {
	if sourceObj == nil {
		return
	}
	workqueue.Done(sourceObj)
	workqueue.Forget(sourceObj)
}

func (bc *BatchCollector) completeSuccessfulWorkItems(workqueue workqueue.RateLimitingInterface, allSourceObjs map[interface{}]struct{}, failedSourceObjs map[interface{}]struct{}, permanentlyFailedSourceObjs map[interface{}]struct{}, eventLogger *zap.SugaredLogger) {
	for obj := range allSourceObjs {
		if _, failed := failedSourceObjs[obj]; failed {
			continue
		}
		if _, permanentlyFailed := permanentlyFailedSourceObjs[obj]; permanentlyFailed {
			continue
		}
		eventLogger.Debugw("Completing work item after successful batch flush")
		bc.completeWorkItem(workqueue, obj)
	}
}

func (bc *BatchCollector) completePermanentFailures(workqueue workqueue.RateLimitingInterface, permanentlyFailedSourceObjs map[interface{}]struct{}) {
	for obj := range permanentlyFailedSourceObjs {
		bc.completeWorkItem(workqueue, obj)
	}
}

func (bc *BatchCollector) requeueFailedWorkItems(workqueue workqueue.RateLimitingInterface, failedSourceObjs map[interface{}]struct{}, allowRequeue bool, eventLogger *zap.SugaredLogger) {
	if len(failedSourceObjs) == 0 {
		return
	}
	for obj := range failedSourceObjs {
		numRequeues := workqueue.NumRequeues(obj)
		if !allowRequeue {
			eventLogger.Warnw("Not requeuing work item after batch flush failure because requeue is disabled", "numRequeues", numRequeues)
			bc.completeWorkItem(workqueue, obj)
			continue
		}
		if numRequeues >= MaxNumRequeues {
			if item, ok := eventItemFromWorkqueueObj(obj); ok {
				eventLogger.Warnw("Giving up on work item after max requeues due to batch flush failure", "numRequeues", numRequeues, "key", item.Key, "kindIndex", item.KindIndex, "eventSource", item.EventSource)
			} else {
				eventLogger.Warnw("Giving up on work item after max requeues due to batch flush failure", "numRequeues", numRequeues)
			}
			bc.completeWorkItem(workqueue, obj)
			continue
		}
		if item, ok := eventItemFromWorkqueueObj(obj); ok {
			eventLogger.Debugw("Requeuing work item due to retryable batch flush failure", "numRequeues", numRequeues, "key", item.Key, "kindIndex", item.KindIndex, "eventSource", item.EventSource)
		} else {
			eventLogger.Debugw("Requeuing work item due to retryable batch flush failure", "numRequeues", numRequeues)
		}
		workqueue.Done(obj)
		workqueue.AddRateLimited(obj)
	}
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

func (bc *BatchCollector) ProcessBatch(controller *Controller, workqueue workqueue.RateLimitingInterface, eventLogger *zap.SugaredLogger, allowRequeue bool) *SyncResult {
	if len(bc.entitiesByBlueprint) == 0 {
		bc.lastFlush = time.Now()
		eventLogger.Debugw("Batch collector has no entities to process", "hasErrors", bc.hasErrors, "controller", controller.Resource.Kind)
		return &SyncResult{
			EntitiesSet:               make(map[string]interface{}),
			RawDataExamples:           make([]interface{}, 0),
			ShouldDeleteStaleEntities: !bc.hasErrors,
		}
	}
	entitiesSet := make(map[string]interface{})
	shouldDeleteStaleEntities := !bc.hasErrors
	failedSourceObjs := make(map[interface{}]struct{})
	permanentlyFailedSourceObjs := make(map[interface{}]struct{})
	allSourceObjs := make(map[interface{}]struct{})
	maxPayloadBytes := config.ApplicationConfig.BulkSyncMaxPayloadBytes
	maxEntitiesPerBlueprintBatch := config.ApplicationConfig.BulkSyncMaxEntitiesPerBatch
	totalEntities := 0
	successCountWithKind := make(map[string]int)
	failedUpsertsCountWithKind := make(map[string]int)
	for _, entitiesWithKind := range bc.entitiesByBlueprint {
		totalEntities += len(entitiesWithKind)
		bc.collectSourceObjs(allSourceObjs, entitiesWithKind)
	}
	eventLogger.Infow("Batch processing", "totalEntities", totalEntities, "blueprintCount", len(bc.entitiesByBlueprint), "maxPayloadBytes", maxPayloadBytes, "maxEntitiesPerBlueprintBatch", maxEntitiesPerBlueprintBatch)

	for blueprint, entitiesWithKind := range bc.entitiesByBlueprint {
		if len(entitiesWithKind) == 0 {
			eventLogger.Debugw("Skipping blueprint with no entities", "blueprint", blueprint)
			continue
		}
		entities := make([]port.EntityRequest, 0)
		entityIdToKind := make(map[string]string)
		for _, entityWithKind := range entitiesWithKind {
			entityIdToKind[fmt.Sprintf("%v", entityWithKind.Entity.Identifier)] = entityWithKind.Kind
			entities = append(entities, entityWithKind.Entity)
		}
		eventLogger.Infow("Processing entities for blueprint", "blueprint", blueprint, "entityCount", len(entities))
		metrics.MeasureDuration(metrics.GetKindLabel(controller.Resource.Kind, nil), metrics.MetricPhaseLoad, func(phase string) (struct{}, error) {
			optimalBatchSize := calculateBulkSize(entities, maxEntitiesPerBlueprintBatch, maxPayloadBytes)
			eventLogger.Infow("Calculated optimal batch size for blueprint", "blueprint", blueprint, "optimalBatchSize", optimalBatchSize)
			for i := 0; i < len(entities); i += optimalBatchSize {
				end := i + optimalBatchSize
				if end > len(entities) {
					end = len(entities)
				}
				batchEntities := entities[i:end]
				batchEntitiesWithKind := entitiesWithKind[i:end]
				bulkResponse, err := controller.portClient.BulkUpsertEntities(context.Background(), blueprint, batchEntities, "", controller.portClient.CreateMissingRelatedEntities)
				if err != nil {
					eventLogger.Warnw(fmt.Sprintf("Bulk upsert failed. Blueprint: %s, Error: %s", blueprint, err.Error()), "blueprint", blueprint, "entityCount", len(batchEntities), "error", err)
					if cli.IsBulkNonRetryableError(err) {
						eventLogger.Warnw("Skipping fallback to individual upserts due to non-retryable error", "blueprint", blueprint, "error", err)
						for _, ewk := range batchEntitiesWithKind {
							failedUpsertsCountWithKind[ewk.Kind]++
							bc.trackPermanentFailure(permanentlyFailedSourceObjs, failedSourceObjs, ewk, "non-retryable bulk upsert error", err, eventLogger)
						}
						shouldDeleteStaleEntities = false
					} else {
						bc.fallbackToIndividualUpserts(controller, batchEntitiesWithKind, &entitiesSet, &shouldDeleteStaleEntities, &successCountWithKind, &failedUpsertsCountWithKind, failedSourceObjs, eventLogger)
					}
					continue
				}
				successCount := 0
				for _, result := range bulkResponse.Entities {
					successCountWithKind[entityIdToKind[result.Identifier]]++
					successCount++
					eventLogger.Infow("Successfully upserted entity", "blueprint", blueprint, "identifier", result.Identifier)
					mockEntity := &port.Entity{
						Identifier: result.Identifier,
						Blueprint:  blueprint,
					}
					entitiesSet[controller.portClient.GetEntityIdentifierKey(mockEntity)] = nil
				}

				if len(bulkResponse.Errors) > 0 {
					eventLogger.Warnw("Bulk upsert had failures", "blueprint", blueprint, "failedCount", len(bulkResponse.Errors), "totalCount", len(batchEntities))
					retryableIdentifiers := make(map[string]bool)
					nonRetryableIdentifiers := make(map[string]bool)
					for _, bulkError := range bulkResponse.Errors {
						if cli.IsNonRetryableStatusCode(bulkError.StatusCode) {
							nonRetryableIdentifiers[bulkError.Identifier] = true
							eventLogger.Warnw("Skipping fallback for entity due to non-retryable error", "blueprint", blueprint, "identifier", bulkError.Identifier, "statusCode", bulkError.StatusCode, "message", bulkError.Message)
						} else {
							retryableIdentifiers[bulkError.Identifier] = true
							eventLogger.Infow("Bulk upsert failed for entity", "blueprint", blueprint, "identifier", bulkError.Identifier, "message", bulkError.Message)
						}
					}
					if len(nonRetryableIdentifiers) > 0 {
						for _, entityWithKind := range batchEntitiesWithKind {
							if nonRetryableIdentifiers[fmt.Sprintf("%v", entityWithKind.Entity.Identifier)] {
								failedUpsertsCountWithKind[entityWithKind.Kind]++
								bc.trackPermanentFailure(permanentlyFailedSourceObjs, failedSourceObjs, entityWithKind, "non-retryable bulk upsert entity error", nil, eventLogger)
							}
						}
						shouldDeleteStaleEntities = false
					}
					failedEntitiesWithKind := make([]EntityWithKind, 0)
					for _, entityWithKind := range batchEntitiesWithKind {
						if retryableIdentifiers[fmt.Sprintf("%v", entityWithKind.Entity.Identifier)] {
							failedEntitiesWithKind = append(failedEntitiesWithKind, entityWithKind)
						}
					}
					if len(failedEntitiesWithKind) > 0 {
						bc.fallbackToIndividualUpserts(controller, failedEntitiesWithKind, &entitiesSet, &shouldDeleteStaleEntities, &successCountWithKind, &failedUpsertsCountWithKind, failedSourceObjs, eventLogger)
					}
				}
				eventLogger.Infow(fmt.Sprintf("Bulk upsert completed for blueprint %s.", blueprint), "blueprint", blueprint, "successCount", successCount, "failedCount", len(bulkResponse.Errors))
			}
			return struct{}{}, nil
		})
	}
	// Clear the batch
	bc.entitiesByBlueprint = make(map[string][]EntityWithKind)
	bc.lastFlush = time.Now()
	bc.completeSuccessfulWorkItems(workqueue, allSourceObjs, failedSourceObjs, permanentlyFailedSourceObjs, eventLogger)
	bc.requeueFailedWorkItems(workqueue, failedSourceObjs, allowRequeue, eventLogger)
	bc.completePermanentFailures(workqueue, permanentlyFailedSourceObjs)

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

func (bc *BatchCollector) fallbackToIndividualUpserts(controller *Controller, entitiesWithKind []EntityWithKind, entitiesSet *map[string]interface{}, shouldDeleteStaleEntities *bool, successCountWithKind *map[string]int, failedUpsertsCountWithKind *map[string]int, failedSourceObjs map[interface{}]struct{}, eventLogger *zap.SugaredLogger) {
	eventLogger.Infow("Falling back to individual upserts", "entityCount", len(entitiesWithKind))

	for _, entityWithKind := range entitiesWithKind {
		handledEntity, err := controller.entityHandler(entityWithKind.Entity, CreateAction, port.ResyncSource, eventLogger)
		if err != nil {
			eventLogger.Errorw("Individual upsert fallback failed", "identifier", entityWithKind.Entity.Identifier, "blueprint", entityWithKind.Entity.Blueprint, "error", err)
			(*failedUpsertsCountWithKind)[entityWithKind.Kind]++
			*shouldDeleteStaleEntities = false
			bc.trackFailedWorkItem(failedSourceObjs, entityWithKind.SourceObj)
		} else if handledEntity != nil {
			(*entitiesSet)[controller.portClient.GetEntityIdentifierKey(handledEntity)] = nil
			eventLogger.Infow("Individual upsert fallback succeeded", "identifier", entityWithKind.Entity.Identifier, "blueprint", entityWithKind.Entity.Blueprint)
			(*successCountWithKind)[entityWithKind.Kind]++
		}
	}
}

func (bc *BatchCollector) ProcessRemaining(controller *Controller, workqueue workqueue.RateLimitingInterface, eventLogger *zap.SugaredLogger) *SyncResult {
	if len(bc.entitiesByBlueprint) == 0 {
		return nil
	}
	return bc.ProcessBatch(controller, workqueue, eventLogger, false)
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
