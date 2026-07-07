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
