package handlers

import (
	"encoding/json"
	"math"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

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
			// Use conservative estimate on error
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
