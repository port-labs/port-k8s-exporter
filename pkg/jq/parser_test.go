package jq

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	_ "github.com/port-labs/port-k8s-exporter/test_utils"
)

var (
	blueprint = "k8s-export-test-bp"
)

func TestJqSearchRelation(t *testing.T) {

	mapping := []port.EntityMapping{
		{
			Identifier: ".metadata.name",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
			Icon:       "\"Microservice\"",
			Team:       "\"Test\"",
			Properties: map[string]string{},
			Relations: map[string]interface{}{
				"k8s-relation": map[string]interface{}{
					"combinator": "\"or\"",
					"rules": []interface{}{
						map[string]interface{}{
							"property": "\"$identifier\"",
							"operator": "\"=\"",
							"value":    "\"e_AgPMYvq1tAs8TuqM\"",
						},
					},
				},
			},
		},
	}
	res, _ := ParseMapRecursively(mapping[0].Relations, nil)
	assert.Equal(t, res, map[string]interface{}{
		"k8s-relation": map[string]interface{}{
			"combinator": "or",
			"rules": []interface{}{
				map[string]interface{}{
					"property": "$identifier",
					"operator": "=",
					"value":    "e_AgPMYvq1tAs8TuqM",
				},
			},
		},
	})

}

func TestJqSearchIdentifier(t *testing.T) {

	mapping := []port.EntityMapping{
		{
			Identifier: map[string]interface{}{
				"combinator": "\"and\"",
				"rules": []interface{}{
					map[string]interface{}{
						"property": "\"prop1\"",
						"operator": "\"in\"",
						"value":    ".values",
					},
				},
			},
			Blueprint: fmt.Sprintf("\"%s\"", blueprint),
		},
	}
	res, _ := ParseMapRecursively(mapping[0].Identifier.(map[string]interface{}), map[string]interface{}{"values": []string{"val1", "val2"}})
	assert.Equal(t, res, map[string]interface{}{
		"combinator": "and",
		"rules": []interface{}{
			map[string]interface{}{
				"property": "prop1",
				"operator": "in",
				"value":    []string{"val1", "val2"},
			},
		},
	})

}

func TestJqSearchTeam(t *testing.T) {
	mapping := []port.EntityMapping{
		{
			Identifier: "\"Frontend-Service\"",
			Blueprint:  fmt.Sprintf("\"%s\"", blueprint),
			Icon:       "\"Microservice\"",
			Team: map[string]interface{}{
				"combinator": "\"and\"",
				"rules": []interface{}{
					map[string]interface{}{
						"property": "\"team\"",
						"operator": "\"in\"",
						"value":    ".values",
					},
				},
			},
		},
	}
	resMap, _ := ParseMapRecursively(mapping[0].Team.(map[string]interface{}), map[string]interface{}{"values": []string{"val1", "val2"}})
	assert.Equal(t, resMap, map[string]interface{}{
		"combinator": "and",
		"rules": []interface{}{
			map[string]interface{}{
				"property": "team",
				"operator": "in",
				"value":    []string{"val1", "val2"},
			},
		},
	})
}

func TestJqEmptyResults(t *testing.T) {
	testObj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name": "test-deployment",
		},
		"spec": map[string]interface{}{
			"replicas": 3,
		},
	}

	t.Run("ParseArray with empty results", func(t *testing.T) {
		// Query that returns empty results
		result, err := ParseArray(".spec.containers", testObj)
		assert.NoError(t, err)
		assert.Equal(t, []interface{}{}, result)
	})

	t.Run("ParseString with non-existent field", func(t *testing.T) {
		// Query for non-existent field
		result, err := ParseString(".metadata.nonexistent", testObj)
		assert.NoError(t, err)
		assert.Equal(t, "", result)
	})

	t.Run("ParseBool with non-existent field", func(t *testing.T) {
		// Query for non-existent boolean field
		result, err := ParseBool(".spec.nonexistent", testObj)
		assert.NoError(t, err)
		assert.Equal(t, false, result)
	})

	t.Run("ParseInterface with non-existent field", func(t *testing.T) {
		// Query for non-existent field
		result, err := ParseInterface(".status.conditions", testObj)
		assert.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("ParseMapInterface with alternative operator", func(t *testing.T) {
		// Query with alternative operator that returns empty
		queries := map[string]string{
			"version": ".metadata.labels.version // \"unknown\"",
			"missing": ".status.nonexistent",
		}
		result, err := ParseMapInterface(queries, testObj)
		assert.NoError(t, err)
		assert.Equal(t, map[string]interface{}{
			"version": "unknown",
			"missing": nil,
		}, result)
	})

	t.Run("Complex query with filters returning empty", func(t *testing.T) {
		// Query that filters but returns nothing
		result, err := ParseArray(".spec.template.spec.containers | select(.name == \"nonexistent\")", testObj)
		assert.NoError(t, err)
		assert.Equal(t, []interface{}{}, result)
	})
}

func TestJqComplexEntityMapping(t *testing.T) {
	// Simulate the complex Kubernetes deployment object from the original error
	kubernetesObj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      "test-deployment",
			"namespace": "default",
			"labels": map[string]interface{}{
				"app": "test-app",
			},
		},
		"spec": map[string]interface{}{
			"replicas": 3,
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "app",
							"image": "nginx:latest",
						},
					},
				},
			},
		},
		"status": map[string]interface{}{
			"availableReplicas": 2,
		},
	}

	t.Run("Entity mapping with complex alternative operators", func(t *testing.T) {
		// This mimics the complex version query from the original error
		versionQuery := ".metadata.labels[\"app_kubernetes_io/version\"] // .metadata.labels.version // .metadata.labels[\"jfrogAppVersion\"] //.metadata.labels.chartVersion //.metadata.labels[\"helm.sh/chart\"] | select(. != null) |split(\"-\") | last |ltrimstr(\"10\")// .spec.template.metadata.labels[\"app_kubernetes_io/version\"] // \"Not found\""
		
		result, err := ParseString(versionQuery, kubernetesObj)
		assert.NoError(t, err)
		// The query returns empty string when no matches are found due to the complex alternative logic
		// This is still a valid result and doesn't cause an error (which was the original problem)
		assert.Equal(t, "", result)
	})

	t.Run("Service relation with multiple alternative selectors", func(t *testing.T) {
		// This mimics the complex Service relation from the original error
		relations := map[string]interface{}{
			"Service": map[string]interface{}{
				"combinator": "\"or\"",
				"rules": []interface{}{
					map[string]interface{}{
						"operator": "\"=\"",
						"property": "\"$identifier\"",
						"value":    ".metadata.labels.applicationName",
					},
					map[string]interface{}{
						"operator": "\"=\"",
						"property": "\"$identifier\"",
						"value":    ".metadata.labels.serviceName",
					},
					map[string]interface{}{
						"operator": "\"=\"",
						"property": "\"$identifier\"",
						"value":    ".spec.template.metadata.labels.serviceName",
					},
					map[string]interface{}{
						"operator": "\"=\"", 			
						"property": "\"$identifier\"",
						"value":    ".metadata.labels.app",
					},
				},
			},
		}

		result, err := ParseMapRecursively(relations, kubernetesObj)
		assert.NoError(t, err)
		
		// Verify the structure is preserved
		serviceRelation := result["Service"].(map[string]interface{})
		assert.Equal(t, "or", serviceRelation["combinator"])
		
		rules := serviceRelation["rules"].([]interface{})
		assert.Len(t, rules, 4)
		
		// Check that the fourth rule (app label) has the actual value
		fourthRule := rules[3].(map[string]interface{})
		assert.Equal(t, "test-app", fourthRule["value"])
		
		// Check that missing labels result in nil values (not errors)
		firstRule := rules[0].(map[string]interface{})
		assert.Nil(t, firstRule["value"]) // applicationName doesn't exist
	})
}
