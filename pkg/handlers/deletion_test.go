package handlers

import (
	"testing"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/stretchr/testify/assert"
)

func TestParseStaticBlueprint(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantFound bool
	}{
		{"yaml style literal", "'\"namespace\"'", "namespace", true},
		{"go style literal", "\"workload\"", "workload", true},
		{"dynamic jq", ".metadata.labels.blueprint", "", false},
		{"jq with pipe", ".kind | ascii_downcase", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseStaticBlueprint(tt.input)
			assert.Equal(t, tt.wantFound, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildBlueprintToKinds(t *testing.T) {
	resources := []port.Resource{
		{
			Kind: "v1/nodes",
			Port: port.Port{
				Entity: port.EntityMappings{
					Mappings: []port.EntityMapping{{Blueprint: "'\"k8s_node\"'"}},
				},
			},
		},
		{
			Kind: "v1/pods",
			Port: port.Port{
				Entity: port.EntityMappings{
					Mappings: []port.EntityMapping{{Blueprint: "'\"k8s_pod\"'"}},
				},
			},
		},
		{
			Kind: "apps/v1/deployments",
			Port: port.Port{
				Entity: port.EntityMappings{
					Mappings: []port.EntityMapping{{Blueprint: "'\"workload\"'"}},
				},
			},
		},
		{
			Kind: "apps/v1/daemonsets",
			Port: port.Port{
				Entity: port.EntityMappings{
					Mappings: []port.EntityMapping{{Blueprint: "'\"workload\"'"}},
				},
			},
		},
	}

	blueprintToKinds := buildBlueprintToKinds(resources)
	assert.True(t, blueprintToKinds["k8s_node"]["v1/nodes"])
	assert.True(t, blueprintToKinds["k8s_pod"]["v1/pods"])
	assert.True(t, blueprintToKinds["workload"]["apps/v1/deployments"])
	assert.True(t, blueprintToKinds["workload"]["apps/v1/daemonsets"])
}

func TestComputeDeletionPlan_NodeSucceedsPodFails(t *testing.T) {
	blueprintToKinds := buildBlueprintToKinds([]port.Resource{
		{Kind: "v1/nodes", Port: port.Port{Entity: port.EntityMappings{Mappings: []port.EntityMapping{{Blueprint: "'\"k8s_node\"'"}}}}},
		{Kind: "v1/pods", Port: port.Port{Entity: port.EntityMappings{Mappings: []port.EntityMapping{{Blueprint: "'\"k8s_pod\"'"}}}}},
	})

	results := []ControllerSyncResult{
		{
			Kind:                      "v1/nodes",
			ShouldDeleteStaleEntities: true,
			EntitiesSet:               map[string]interface{}{"k8s_node;node-1": nil},
		},
		{
			Kind:                      "v1/pods",
			ShouldDeleteStaleEntities: false,
			EntitiesSet:               map[string]interface{}{},
		},
	}

	mergedSet, eligible, skipped := computeDeletionPlan(results, blueprintToKinds)

	assert.Equal(t, map[string]interface{}{"k8s_node;node-1": nil}, mergedSet)
	assert.True(t, eligible["k8s_node"])
	assert.False(t, eligible["k8s_pod"])
	assert.Equal(t, []string{"v1/pods"}, skipped)
}

func TestComputeDeletionPlan_SharedBlueprintOneKindFails(t *testing.T) {
	blueprintToKinds := buildBlueprintToKinds([]port.Resource{
		{Kind: "apps/v1/deployments", Port: port.Port{Entity: port.EntityMappings{Mappings: []port.EntityMapping{{Blueprint: "'\"workload\"'"}}}}},
		{Kind: "apps/v1/daemonsets", Port: port.Port{Entity: port.EntityMappings{Mappings: []port.EntityMapping{{Blueprint: "'\"workload\"'"}}}}},
	})

	results := []ControllerSyncResult{
		{
			Kind:                      "apps/v1/deployments",
			ShouldDeleteStaleEntities: true,
			EntitiesSet:               map[string]interface{}{"workload;deploy-1": nil},
		},
		{
			Kind:                      "apps/v1/daemonsets",
			ShouldDeleteStaleEntities: false,
			EntitiesSet:               map[string]interface{}{},
		},
	}

	_, eligible, skipped := computeDeletionPlan(results, blueprintToKinds)

	assert.False(t, eligible["workload"])
	assert.Equal(t, []string{"apps/v1/daemonsets"}, skipped)
}

func TestComputeDeletionPlan_SharedBlueprintBothKindsSucceed(t *testing.T) {
	blueprintToKinds := buildBlueprintToKinds([]port.Resource{
		{Kind: "apps/v1/deployments", Port: port.Port{Entity: port.EntityMappings{Mappings: []port.EntityMapping{{Blueprint: "'\"workload\"'"}}}}},
		{Kind: "apps/v1/daemonsets", Port: port.Port{Entity: port.EntityMappings{Mappings: []port.EntityMapping{{Blueprint: "'\"workload\"'"}}}}},
	})

	results := []ControllerSyncResult{
		{
			Kind:                      "apps/v1/deployments",
			ShouldDeleteStaleEntities: true,
			EntitiesSet:               map[string]interface{}{"workload;deploy-1": nil},
		},
		{
			Kind:                      "apps/v1/daemonsets",
			ShouldDeleteStaleEntities: true,
			EntitiesSet:               map[string]interface{}{"workload;daemon-1": nil},
		},
	}

	mergedSet, eligible, skipped := computeDeletionPlan(results, blueprintToKinds)

	assert.True(t, eligible["workload"])
	assert.Empty(t, skipped)
	assert.Equal(t, map[string]interface{}{
		"workload;deploy-1": nil,
		"workload;daemon-1": nil,
	}, mergedSet)
}

func TestComputeDeletionPlan_DynamicBlueprintFromEntitySet(t *testing.T) {
	blueprintToKinds := make(map[string]map[string]bool)

	results := []ControllerSyncResult{
		{
			Kind:                      "custom/v1/widgets",
			ShouldDeleteStaleEntities: true,
			EntitiesSet:               map[string]interface{}{"dynamic-bp;widget-1": nil},
		},
	}

	_, eligible, _ := computeDeletionPlan(results, blueprintToKinds)

	assert.True(t, eligible["dynamic-bp"])
}

func TestSortBlueprintsForDeletion_ChildBeforeParent(t *testing.T) {
	relations := map[string]map[string]port.Relation{
		"k8s_pod": {
			"node": {Target: "k8s_node"},
		},
		"k8s_node": nil,
		"k8s_workload": {
			"namespace": {Target: "namespace"},
		},
		"namespace": {
			"Cluster": {Target: "cluster"},
		},
		"cluster": nil,
	}

	order := sortBlueprintsForDeletion(relations)

	podIdx := indexOf(order, "k8s_pod")
	nodeIdx := indexOf(order, "k8s_node")
	workloadIdx := indexOf(order, "k8s_workload")
	namespaceIdx := indexOf(order, "namespace")
	clusterIdx := indexOf(order, "cluster")

	assert.Less(t, podIdx, nodeIdx)
	assert.Less(t, workloadIdx, namespaceIdx)
	assert.Less(t, namespaceIdx, clusterIdx)
}

func TestSortBlueprintsForDeletion_NoRelationsUsesStableOrder(t *testing.T) {
	order := sortBlueprintsForDeletion(map[string]map[string]port.Relation{
		"beta":  nil,
		"alpha": nil,
	})
	assert.Equal(t, []string{"alpha", "beta"}, order)
}

func TestPopMinReady_SingleElement(t *testing.T) {
	current, remaining := popMinReady([]string{"only"})

	assert.Equal(t, "only", current)
	assert.Empty(t, remaining)
}

func TestPopMinReady_PicksAlphabeticallyFirst(t *testing.T) {
	current, remaining := popMinReady([]string{"zebra", "alpha", "mango"})

	assert.Equal(t, "alpha", current)
	assert.Equal(t, []string{"zebra", "mango"}, remaining)
}

func TestPopMinReady_RemovesSelectedElementOnly(t *testing.T) {
	_, remaining := popMinReady([]string{"beta", "alpha"})

	assert.Equal(t, []string{"beta"}, remaining)
}

func TestPopMinReady_SequentialPicksAreSorted(t *testing.T) {
	ready := []string{"charlie", "alpha", "bravo"}
	picked := make([]string, 0, len(ready))

	for len(ready) > 0 {
		var current string
		current, ready = popMinReady(ready)
		picked = append(picked, current)
	}

	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, picked)
	assert.Empty(t, ready)
}

func indexOf(items []string, target string) int {
	for i, item := range items {
		if item == target {
			return i
		}
	}
	return -1
}
