package jq

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"

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
