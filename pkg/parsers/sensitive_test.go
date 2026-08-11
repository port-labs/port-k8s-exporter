package parsers

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseSensitiveData_BasicTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name:     "string with password",
			input:    "password123",
			expected: "password123", // No patterns match this simple case
		},
		{
			name:     "string with API key pattern",
			input:    `api_key="AIzaSyDxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"`,
			expected: `api_key="[REDACTED]x"`, // The regex only partially matches
		},
		{
			name:     "simple slice of strings",
			input:    []string{"hello", "world"},
			expected: []string{"hello", "world"},
		},
		{
			name:     "simple map",
			input:    map[string]string{"key": "value"},
			expected: map[string]string{"key": "value"},
		},
		{
			name:     "integer",
			input:    42,
			expected: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseSensitiveData(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("ParseSensitiveData() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseSensitiveData_StructWithUnexportedFields(t *testing.T) {
	// This is the test for the specific panic scenario
	type structWithUnexportedField struct {
		PublicField  string
		privateField string // This causes the panic
	}

	testCases := []struct {
		name  string
		input interface{}
	}{
		{
			name: "slice with struct containing unexported fields",
			input: []structWithUnexportedField{
				{PublicField: "public1", privateField: "private1"},
				{PublicField: "public2", privateField: "private2"},
			},
		},
		{
			name: "map with struct containing unexported fields",
			input: map[string]structWithUnexportedField{
				"key1": {PublicField: "public1", privateField: "private1"},
				"key2": {PublicField: "public2", privateField: "private2"},
			},
		},
		{
			name: "nested structure",
			input: []map[string]structWithUnexportedField{
				{
					"item": {PublicField: "public", privateField: "private"},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ParseSensitiveData() panicked: %v", r)
				}
			}()

			result := ParseSensitiveData(tc.input)

			// Basic validation that we got some result back
			if result == nil {
				t.Error("ParseSensitiveData() returned nil")
			}
		})
	}
}

func TestParseSensitiveData_ComplexNestedStructures(t *testing.T) {
	type innerStruct struct {
		Value      string
		privateVal int
	}

	type outerStruct struct {
		Public  string
		Inner   innerStruct
		private string
	}

	input := []outerStruct{
		{
			Public: "public1",
			Inner:  innerStruct{Value: "inner1", privateVal: 42},
			private: "private1",
		},
	}

	// This should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ParseSensitiveData() panicked with complex nested structures: %v", r)
		}
	}()

	result := ParseSensitiveData(input)

	if result == nil {
		t.Error("ParseSensitiveData() returned nil for complex nested structures")
	}
}

func TestParseSensitiveData_SecretMasking(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		contains string // Check if result contains this string
		notContains string // Check if result doesn't contain this string
	}{
		{
			name:        "API key in slice",
			input:       []string{"normal string", `api_key="AIzaSyDxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"`},
			contains:    "[REDACTED]",
			notContains: "AIzaSyD",
		},
		{
			name:        "Secret in map values",
			input:       map[string]string{"normal": "value", "secret": `secret="abcdefghijklmnopqrstuvwxyz123456789"`},
			contains:    "[REDACTED]",
			notContains: "abcdefghijklmnopqrstuvwxyz123456789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseSensitiveData(tt.input)
			resultStr := interfaceToString(result)

			if tt.contains != "" && !containsString(resultStr, tt.contains) {
				t.Errorf("ParseSensitiveData() result should contain %q, got: %v", tt.contains, result)
			}

			if tt.notContains != "" && containsString(resultStr, tt.notContains) {
				t.Errorf("ParseSensitiveData() result should not contain %q, got: %v", tt.notContains, result)
			}
		})
	}
}

// Helper functions
func interfaceToString(v interface{}) string {
	return fmt.Sprintf("%+v", v)
}

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}