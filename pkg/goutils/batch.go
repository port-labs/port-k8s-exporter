package goutils

import (
	"encoding/json"
	"math"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

const (
	DefaultMaxBatchLength = 100
	DefaultMaxBatchSize   = 1024 * 1024 // 1MB
	SampleSize           = 10
	SizeMultiplier       = 1.5
)

// CalculateEntitiesBatchSize determines the optimal batch size for bulk entity creation
// based on the average size of a sample of entities
func CalculateEntitiesBatchSize(entities []port.EntityRequest) int {
	if len(entities) == 0 {
		return 1
	}

	// Calculate average entity size from a sample
	sampleSize := min(SampleSize, len(entities))
	sampleEntities := entities[:sampleSize]
	
	var totalSize int64
	for _, entity := range sampleEntities {
		entityBytes, err := json.Marshal(entity)
		if err != nil {
			// If we can't marshal, use a conservative estimate
			return 1
		}
		totalSize += int64(len(entityBytes))
	}
	
	averageEntitySize := float64(totalSize) / float64(sampleSize)
	
	// Use a conservative estimate (1.5x the average) to ensure we stay under the limit
	estimatedEntitySize := int(math.Ceil(averageEntitySize * SizeMultiplier))
	maxEntitiesPerBatch := min(
		DefaultMaxBatchLength,
		int(math.Floor(float64(DefaultMaxBatchSize)/float64(estimatedEntitySize))),
	)
	
	return maxEntitiesPerBatch
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
} 