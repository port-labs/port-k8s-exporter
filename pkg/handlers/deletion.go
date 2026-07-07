package handlers

import (
	"strings"

	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
)

type ControllerSyncResult struct {
	Kind                      string
	EntitiesSet               map[string]interface{}
	ShouldDeleteStaleEntities bool
}

type FullResyncResults struct {
	ControllerResults []ControllerSyncResult
}

func parseStaticBlueprint(blueprintJQ string) (string, bool) {
	trimmed := strings.TrimSpace(blueprintJQ)
	trimmed = strings.Trim(trimmed, `"'`)
	if trimmed == "" || strings.ContainsAny(trimmed, ".|(") {
		return "", false
	}
	return trimmed, true
}

func buildBlueprintToKinds(resources []port.Resource) map[string]map[string]bool {
	blueprintToKinds := make(map[string]map[string]bool)
	for _, resource := range resources {
		for _, mapping := range resource.Port.Entity.Mappings {
			blueprint, ok := parseStaticBlueprint(mapping.Blueprint)
			if !ok {
				continue
			}
			if blueprintToKinds[blueprint] == nil {
				blueprintToKinds[blueprint] = make(map[string]bool)
			}
			blueprintToKinds[blueprint][resource.Kind] = true
		}
	}
	return blueprintToKinds
}

func augmentBlueprintToKindsFromResults(blueprintToKinds map[string]map[string]bool, results []ControllerSyncResult) {
	for _, result := range results {
		if !result.ShouldDeleteStaleEntities {
			continue
		}
		for key := range result.EntitiesSet {
			blueprint, _, ok := strings.Cut(key, ";")
			if !ok || blueprint == "" {
				continue
			}
			if blueprintToKinds[blueprint] == nil {
				blueprintToKinds[blueprint] = make(map[string]bool)
			}
			blueprintToKinds[blueprint][result.Kind] = true
		}
	}
}

func computeDeletionPlan(
	results []ControllerSyncResult,
	blueprintToKinds map[string]map[string]bool,
) (mergedSet map[string]interface{}, eligibleBlueprints map[string]bool, skippedKinds []string) {
	kindSucceeded := make(map[string]bool, len(results))
	successfulEntitySets := make([]map[string]interface{}, 0, len(results))

	for _, result := range results {
		kindSucceeded[result.Kind] = result.ShouldDeleteStaleEntities
		if !result.ShouldDeleteStaleEntities {
			skippedKinds = append(skippedKinds, result.Kind)
			continue
		}
		successfulEntitySets = append(successfulEntitySets, result.EntitiesSet)
	}

	augmentBlueprintToKindsFromResults(blueprintToKinds, results)

	mergedSet = goutils.MergeMaps(successfulEntitySets...)
	eligibleBlueprints = make(map[string]bool)
	for blueprint, kinds := range blueprintToKinds {
		eligible := true
		for kind := range kinds {
			if !kindSucceeded[kind] {
				eligible = false
				break
			}
		}
		if eligible {
			eligibleBlueprints[blueprint] = true
		}
	}

	return mergedSet, eligibleBlueprints, skippedKinds
}
