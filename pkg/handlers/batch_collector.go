package handlers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/k8s"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
	"github.com/port-labs/port-k8s-exporter/pkg/metrics"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

// EntityWithKind represents an entity with its associated kind
type EntityWithKind struct {
	Entity port.EntityRequest
	Kind   string
}

// BatchCollector handles batched entity operations with thread safety
type BatchCollector struct {
	entitiesByBlueprint map[string][]EntityWithKind
	maxBatchSize        int
	timeout             time.Duration
	lastFlush           time.Time
	errorState          bool
	guards              batchGuards
}

// NewBatchCollector creates a new thread-safe batch collector
func NewBatchCollector(maxBatchSize int, timeout time.Duration) *BatchCollector {
	return &BatchCollector{
		entitiesByBlueprint: make(map[string][]EntityWithKind),
		maxBatchSize:        maxBatchSize,
		timeout:             timeout,
		lastFlush:           time.Now(),
		errorState:          false,
		guards:              batchGuards{},
	}
}

// AddEntity safely adds an entity to the batch
func (bc *BatchCollector) AddEntity(entity port.EntityRequest, kind string) {
	bc.addEntityToBlueprint(entity.Blueprint, EntityWithKind{
		Entity: entity,
		Kind:   kind,
	})
}

// MarkError safely marks that an error occurred
func (bc *BatchCollector) MarkError() {
	bc.setErrorState(true)
}

// HasErrors safely checks if errors occurred
func (bc *BatchCollector) HasErrors() bool {
	bc.guards.errorsMu.RLock()
	defer bc.guards.errorsMu.RUnlock()
	return bc.errorState
}

// getHasErrors is an internal method to check error state
func (bc *BatchCollector) getHasErrors() bool {
	bc.guards.errorsMu.RLock()
	defer bc.guards.errorsMu.RUnlock()
	return bc.errorState
}

// ShouldFlush checks if the batch should be flushed
func (bc *BatchCollector) ShouldFlush() bool {
	bc.guards.entitiesMu.RLock()
	totalEntities := 0
	for _, entities := range bc.entitiesByBlueprint {
		totalEntities += len(entities)
	}
	bc.guards.entitiesMu.RUnlock()

	lastFlush := bc.getLastFlush()
	return totalEntities >= bc.maxBatchSize || time.Since(lastFlush) > bc.timeout
}

// ProcessBatch processes all entities in the current batch
func (bc *BatchCollector) ProcessBatch(controller *k8s.Controller) *SyncResult {
	// Get a safe copy of the entities map
	entities := bc.getEntitiesByBlueprint()
	if len(entities) == 0 {
		logger.Debugw("Batch collector has no entities to process", "hasErrors", bc.getErrorState(), "controller", controller.Resource.Kind)
		return &SyncResult{
			EntitiesSet:               make(map[string]interface{}),
			RawDataExamples:           make([]interface{}, 0),
			ShouldDeleteStaleEntities: !bc.getErrorState(),
		}
	}

	entitiesSet := make(map[string]interface{})
	shouldDeleteStaleEntities := !bc.getErrorState()
	maxPayloadBytes := config.ApplicationConfig.BulkSyncMaxPayloadBytes
	maxEntitiesPerBlueprintBatch := config.ApplicationConfig.BulkSyncMaxEntitiesPerBatch

	// Count total entities safely
	totalEntities := 0
	for _, entitiesWithKind := range entities {
		totalEntities += len(entitiesWithKind)
	}

	logger.Infow("Batch processing", "totalEntities", totalEntities, "blueprintCount", len(entities), "maxPayloadBytes", maxPayloadBytes, "maxEntitiesPerBlueprintBatch", maxEntitiesPerBlueprintBatch)

	// Track success/failure counts with thread safety
	var successCountMu sync.Mutex
	var failedCountMu sync.Mutex
	successCountWithKind := make(map[string]int)
	failedUpsertsCountWithKind := make(map[string]int)

	// Process each blueprint's entities
	var wg sync.WaitGroup
	for blueprint, entitiesWithKind := range entities {
		if len(entitiesWithKind) == 0 {
			logger.Debugw("Skipping blueprint with no entities", "blueprint", blueprint)
			continue
		}

		wg.Add(1)
		go func(blueprint string, entities []EntityWithKind) {
			defer wg.Done()

			// Process entities for this blueprint
			batchEntities := make([]port.EntityRequest, len(entities))
			entityIdToKind := make(map[string]string)
			for i, entityWithKind := range entities {
				batchEntities[i] = entityWithKind.Entity
				entityIdToKind[fmt.Sprintf("%v", entityWithKind.Entity.Identifier)] = entityWithKind.Kind
			}

			logger.Infow("Processing entities for blueprint", "blueprint", blueprint, "entityCount", len(batchEntities))

			// Calculate optimal batch size
			optimalBatchSize := calculateBulkSize(batchEntities, maxEntitiesPerBlueprintBatch, maxPayloadBytes)
			logger.Infow("Calculated optimal batch size for blueprint", "blueprint", blueprint, "optimalBatchSize", optimalBatchSize)

			// Process in batches
			for i := 0; i < len(batchEntities); i += optimalBatchSize {
				end := i + optimalBatchSize
				if end > len(batchEntities) {
					end = len(batchEntities)
				}

				currentBatch := batchEntities[i:end]
				currentBatchWithKind := entities[i:end]

				// Process batch with retries
				err := bc.processBatchWithRetry(controller, blueprint, currentBatch, currentBatchWithKind, &entitiesSet, &successCountWithKind, &failedUpsertsCountWithKind, &successCountMu, &failedCountMu)
				if err != nil {
					logger.Errorw("Failed to process batch after retries", "blueprint", blueprint, "error", err)
					bc.setErrorState(true)
				}
			}
		}(blueprint, entitiesWithKind)
	}

	// Wait for all batches to complete
	wg.Wait()

	// Update metrics asynchronously
	go func() {
		successCountMu.Lock()
		for kindLabel, count := range successCountWithKind {
			metrics.AddObjectCount(kindLabel, metrics.MetricLoadedResult, metrics.MetricPhaseLoad, float64(count))
		}
		successCountMu.Unlock()
	}()

	go func() {
		failedCountMu.Lock()
		for kindLabel, count := range failedUpsertsCountWithKind {
			metrics.AddObjectCount(kindLabel, metrics.MetricFailedResult, metrics.MetricPhaseLoad, float64(count))
		}
		failedCountMu.Unlock()
	}()

	// Clear the batch and update last flush time
	bc.setEntitiesByBlueprint(make(map[string][]EntityWithKind))
	bc.setLastFlush(time.Now())

	return &SyncResult{
		EntitiesSet:               entitiesSet,
		RawDataExamples:           make([]interface{}, 0),
		ShouldDeleteStaleEntities: shouldDeleteStaleEntities,
	}
}

// processBatchWithRetry attempts to process a batch with retries
func (bc *BatchCollector) processBatchWithRetry(
	controller *k8s.Controller,
	blueprint string,
	batchEntities []port.EntityRequest,
	batchEntitiesWithKind []EntityWithKind,
	entitiesSet *map[string]interface{},
	successCountWithKind *map[string]int,
	failedUpsertsCountWithKind *map[string]int,
	successCountMu *sync.Mutex,
	failedCountMu *sync.Mutex,
) error {
	maxRetries := 3
	var lastErr error

	for retry := 0; retry < maxRetries; retry++ {
		if retry > 0 {
			// Exponential backoff
			time.Sleep(time.Duration(1<<uint(retry)) * time.Second)
		}

		bulkResponse, err := controller.GetPortClient().BulkUpsertEntities(
			context.Background(),
			blueprint,
			batchEntities,
			"",
			controller.GetPortClient().CreateMissingRelatedEntities,
		)
		if err != nil {
			lastErr = err
			logger.Warnw(fmt.Sprintf("Bulk upsert failed (attempt %d). Blueprint: %s, Error: %s", retry+1, blueprint, err.Error()),
				"blueprint", blueprint, "entityCount", len(batchEntities), "error", err)
			continue
		}

		// Create identifier to kind mapping
		entityIdToKind := make(map[string]string)
		for _, entityWithKind := range batchEntitiesWithKind {
			entityIdToKind[fmt.Sprintf("%v", entityWithKind.Entity.Identifier)] = entityWithKind.Kind
		}

		// Process successful entities
		successCountMu.Lock()
		for _, result := range bulkResponse.Entities {
			if kind, ok := entityIdToKind[result.Identifier]; ok {
				(*successCountWithKind)[kind]++
			}
			mockEntity := &port.Entity{
				Identifier: result.Identifier,
				Blueprint:  blueprint,
			}
			(*entitiesSet)[controller.GetPortClient().GetEntityIdentifierKey(mockEntity)] = nil
		}
		successCountMu.Unlock()

		// Handle partial failures
		if len(bulkResponse.Errors) > 0 {
			logger.Warnw("Bulk upsert had failures",
				"blueprint", blueprint,
				"failedCount", len(bulkResponse.Errors),
				"totalCount", len(batchEntities))

			// Track failed entities
			failedIdentifiers := make(map[string]bool)
			for _, bulkError := range bulkResponse.Errors {
				failedIdentifiers[bulkError.Identifier] = true
				logger.Infow("Bulk upsert failed for entity",
					"blueprint", blueprint,
					"identifier", bulkError.Identifier,
					"message", bulkError.Message)
			}

			// Retry failed entities individually
			var failedEntities []EntityWithKind
			for _, entityWithKind := range batchEntitiesWithKind {
				if failedIdentifiers[fmt.Sprintf("%v", entityWithKind.Entity.Identifier)] {
					failedEntities = append(failedEntities, entityWithKind)
				}
			}

			if len(failedEntities) > 0 {
				bc.fallbackToIndividualUpserts(controller, failedEntities, entitiesSet, failedUpsertsCountWithKind, failedCountMu)
			}
		}

		return nil // Success
	}

	return fmt.Errorf("failed after %d retries: %v", maxRetries, lastErr)
}

// fallbackToIndividualUpserts handles failed entities one by one
func (bc *BatchCollector) fallbackToIndividualUpserts(
	controller *k8s.Controller,
	failedEntities []EntityWithKind,
	entitiesSet *map[string]interface{},
	failedUpsertsCountWithKind *map[string]int,
	failedCountMu *sync.Mutex,
) {
	logger.Infow("Falling back to individual upserts", "entityCount", len(failedEntities))

	var wg sync.WaitGroup
	for _, entityWithKind := range failedEntities {
		wg.Add(1)
		go func(entity EntityWithKind) {
			defer wg.Done()

			// Try individual upsert with retries
			var err error
			var handledEntity *port.Entity
			for retry := 0; retry < 3; retry++ {
				if retry > 0 {
					time.Sleep(time.Duration(1<<uint(retry)) * time.Second)
				}

				handledEntity, err = controller.HandleEntity(entity.Entity, k8s.EventActionType(CreateAction), port.ResyncSource)
				if err == nil {
					break
				}
			}

			if err != nil {
				logger.Errorw("Individual upsert fallback failed",
					"identifier", entity.Entity.Identifier,
					"blueprint", entity.Entity.Blueprint,
					"error", err)

				failedCountMu.Lock()
				(*failedUpsertsCountWithKind)[entity.Kind]++
				failedCountMu.Unlock()

				bc.setErrorState(true)
			} else if handledEntity != nil {
				(*entitiesSet)[controller.GetPortClient().GetEntityIdentifierKey(handledEntity)] = nil
				logger.Infow("Individual upsert fallback succeeded",
					"identifier", entity.Entity.Identifier,
					"blueprint", entity.Entity.Blueprint)
			}
		}(entityWithKind)
	}

	wg.Wait()
}

// ProcessRemaining processes any remaining entities in the batch
func (bc *BatchCollector) ProcessRemaining(controller *k8s.Controller) *SyncResult {
	entities := bc.getEntitiesByBlueprint()
	if len(entities) == 0 {
		return nil
	}
	return bc.ProcessBatch(controller)
}
