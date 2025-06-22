package goutils

import (
	"reflect"
	"testing"
)

func TestMergeMaps(t *testing.T) {
	tests := []struct {
		name     string
		maps     []map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name:     "empty maps",
			maps:     []map[string]interface{}{},
			expected: map[string]interface{}{},
		},
		{
			name: "single map",
			maps: []map[string]interface{}{
				{"key1": "value1", "key2": "value2"},
			},
			expected: map[string]interface{}{"key1": "value1", "key2": "value2"},
		},
		{
			name: "two maps no overlap",
			maps: []map[string]interface{}{
				{"key1": "value1"},
				{"key2": "value2"},
			},
			expected: map[string]interface{}{"key1": "value1", "key2": "value2"},
		},
		{
			name: "two maps with overlap - last wins",
			maps: []map[string]interface{}{
				{"key1": "value1", "key2": "original"},
				{"key2": "overridden", "key3": "value3"},
			},
			expected: map[string]interface{}{"key1": "value1", "key2": "overridden", "key3": "value3"},
		},
		{
			name: "multiple maps with overlaps",
			maps: []map[string]interface{}{
				{"a": 1, "b": 2},
				{"b": 20, "c": 3},
				{"c": 30, "d": 4},
			},
			expected: map[string]interface{}{"a": 1, "b": 20, "c": 30, "d": 4},
		},
		{
			name: "empty map in the middle",
			maps: []map[string]interface{}{
				{"key1": "value1"},
				{},
				{"key2": "value2"},
			},
			expected: map[string]interface{}{"key1": "value1", "key2": "value2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeMaps(tt.maps...)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("MergeMaps() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMergeMapsWithStringType(t *testing.T) {
	// Test with specific string type
	map1 := map[string]string{"key1": "value1", "common": "first"}
	map2 := map[string]string{"key2": "value2", "common": "second"}

	result := MergeMaps(map1, map2)
	expected := map[string]string{"key1": "value1", "key2": "value2", "common": "second"}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("MergeMaps() with string type = %v, want %v", result, expected)
	}
}

func TestMergeMapsWithIntType(t *testing.T) {
	// Test with specific int type
	map1 := map[string]int{"key1": 1, "common": 10}
	map2 := map[string]int{"key2": 2, "common": 20}

	result := MergeMaps(map1, map2)
	expected := map[string]int{"key1": 1, "key2": 2, "common": 20}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("MergeMaps() with int type = %v, want %v", result, expected)
	}
}

type TestStruct struct {
	Name   string `json:"name"`
	Age    int    `json:"age"`
	Active bool   `json:"active"`
	Score  *int   `json:"score,omitempty"`
}

type NestedStruct struct {
	ID   int        `json:"id"`
	User TestStruct `json:"user"`
	Tags []string   `json:"tags"`
}

func TestStructToMap(t *testing.T) {
	score := 95

	tests := []struct {
		name     string
		input    interface{}
		expected map[string]interface{}
		wantErr  bool
	}{
		{
			name: "simple struct",
			input: TestStruct{
				Name:   "John",
				Age:    30,
				Active: true,
				Score:  &score,
			},
			expected: map[string]interface{}{
				"name":   "John",
				"age":    float64(30), // JSON unmarshaling converts numbers to float64
				"active": true,
				"score":  float64(95),
			},
			wantErr: false,
		},
		{
			name: "struct with nil pointer",
			input: TestStruct{
				Name:   "Jane",
				Age:    25,
				Active: false,
				Score:  nil,
			},
			expected: map[string]interface{}{
				"name":   "Jane",
				"age":    float64(25),
				"active": false,
			},
			wantErr: false,
		},
		{
			name: "nested struct",
			input: NestedStruct{
				ID: 1,
				User: TestStruct{
					Name:   "Alice",
					Age:    28,
					Active: true,
				},
				Tags: []string{"admin", "user"},
			},
			expected: map[string]interface{}{
				"id": float64(1),
				"user": map[string]interface{}{
					"name":   "Alice",
					"age":    float64(28),
					"active": true,
				},
				"tags": []interface{}{"admin", "user"},
			},
			wantErr: false,
		},
		{
			name:     "empty struct",
			input:    struct{}{},
			expected: map[string]interface{}{},
			wantErr:  false,
		},
		{
			name: "map input",
			input: map[string]interface{}{
				"key1": "value1",
				"key2": 42,
			},
			expected: map[string]interface{}{
				"key1": "value1",
				"key2": float64(42), // JSON unmarshaling converts numbers to float64
			},
			wantErr: false,
		},
		{
			name:     "primitive type",
			input:    "simple string",
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := StructToMap(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("StructToMap() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("StructToMap() unexpected error: %v", err)
				return
			}

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("StructToMap() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestStructToMapWithInvalidJSON(t *testing.T) {
	// Test with a type that can't be marshaled to JSON
	invalidInput := make(chan int)

	_, err := StructToMap(invalidInput)
	if err == nil {
		t.Errorf("StructToMap() with invalid input should return error")
	}
}
