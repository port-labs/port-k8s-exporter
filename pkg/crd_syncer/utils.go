package crdsyncer

import (
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func ShallowJsonSchema(schema *v1.JSONSchemaProps, separator string) *v1.JSONSchemaProps {
	clonedSchema := schema.DeepCopy()
	shallowProperties(clonedSchema, "", separator, clonedSchema)

	if schema.Type == "object" {
		return &v1.JSONSchemaProps{
			Type:       "object",
			Properties: clonedSchema.Properties,
			Required:   shallowRequired(schema, "", separator),
		}
	}

	return schema
}

func shallowProperties(schema *v1.JSONSchemaProps, parent string, seperator string, originalSchema *v1.JSONSchemaProps) {
	for k, v := range schema.Properties {
		shallowedKey := k

		if parent != "" {
			shallowedKey = parent + seperator + k
		}

		if v.Type != "object" {
			originalSchema.Properties[shallowedKey] = v
		} else {
			shallowProperties(&v, shallowedKey, seperator, originalSchema)
			delete(originalSchema.Properties, k)
		}
	}
}

// shallowRequired recursively traverses the JSONSchemaProps and returns a list of required fields with nested field names concatenated by the provided separator.
func shallowRequired(schema *v1.JSONSchemaProps, prefix, separator string) []string {
	var requiredFields []string

	for _, field := range schema.Required {
		if propSchema, ok := schema.Properties[field]; ok {
			fullFieldName := field
			if prefix != "" {
				fullFieldName = prefix + separator + field
			}

			if propSchema.Type == "object" {
				// Recursively process nested objects but don't add the object field itself
				nestedRequiredFields := shallowRequired(&propSchema, fullFieldName, separator)
				requiredFields = append(requiredFields, nestedRequiredFields...)
			} else {
				// Add non-object fields to the required list
				requiredFields = append(requiredFields, fullFieldName)
			}
		}
	}

	return requiredFields
}
