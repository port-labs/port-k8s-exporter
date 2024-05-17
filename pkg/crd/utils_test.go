package crd

import (
	"reflect"
	"testing"

	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestCRD_crd_shallowNestedSchema(t *testing.T) {
	originalSchema := &v1.JSONSchemaProps{
		Type: "object",
		Properties: map[string]v1.JSONSchemaProps{
			"spec": {
				Type: "object",
				Properties: map[string]v1.JSONSchemaProps{
					"stringProperty": {
						Type: "string",
					},
					"intProperty": {
						Type: "integer",
					},
					"boolProperty": {
						Type: "boolean",
					},
					"nestedProperty": {
						Type: "object",
						Properties: map[string]v1.JSONSchemaProps{
							"nestedStringProperty": {
								Type: "string",
							},
							"nestedIntProperty": {
								Type: "integer",
							},
						},
						Required: []string{"nestedStringProperty"},
					},
					"multiNestedProperty": {
						Type: "object",
						Properties: map[string]v1.JSONSchemaProps{
							"nestedObjectProperty": {
								Type: "object",
								Properties: map[string]v1.JSONSchemaProps{
									"nestedStringProperty": {
										Type: "string",
									},
								},
								Required: []string{"nestedStringProperty"},
							},
						},
						Required: []string{},
					},
				},
				Required: []string{"stringProperty", "nestedProperty", "multiNestedProperty"},
			},
		},
		Required: []string{"spec"},
	}

	shallowedSchema := ShallowJsonSchema(originalSchema, "__")

	expectedSchema := &v1.JSONSchemaProps{
		Type: "object",
		Properties: map[string]v1.JSONSchemaProps{
			"spec__stringProperty": {
				Type: "string",
			},
			"spec__intProperty": {
				Type: "integer",
			},
			"spec__boolProperty": {
				Type: "boolean",
			},
			"spec__nestedProperty__nestedStringProperty": {
				Type: "string",
			},
			"spec__nestedProperty__nestedIntProperty": {
				Type: "integer",
			},
			"spec__multiNestedProperty__nestedObjectProperty__nestedStringProperty": {
				Type: "string",
			},
		},
		Required: []string{"spec__stringProperty", "spec__nestedProperty__nestedStringProperty"},
	}

	if reflect.DeepEqual(shallowedSchema, expectedSchema) {
		t.Logf("Shallowed schema is as expected")
	} else {
		t.Errorf("Shallowed schema is not as expected")
	}
}
