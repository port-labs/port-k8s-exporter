package goutils

import (
	"reflect"
	"testing"
)

func TestFilter(t *testing.T) {
	tests := []struct {
		name     string
		slice    []int
		item     int
		expected []int
	}{
		{
			name:     "filter existing item",
			slice:    []int{1, 2, 3, 4, 5},
			item:     3,
			expected: []int{1, 2, 4, 5},
		},
		{
			name:     "filter non-existing item",
			slice:    []int{1, 2, 3, 4, 5},
			item:     6,
			expected: []int{1, 2, 3, 4, 5},
		},
		{
			name:     "filter from empty slice",
			slice:    []int{},
			item:     1,
			expected: []int{},
		},
		{
			name:     "filter all occurrences",
			slice:    []int{1, 2, 2, 3, 2, 4},
			item:     2,
			expected: []int{1, 3, 4},
		},
		{
			name:     "filter single element slice - item present",
			slice:    []int{5},
			item:     5,
			expected: []int{},
		},
		{
			name:     "filter single element slice - item not present",
			slice:    []int{5},
			item:     3,
			expected: []int{5},
		},
		{
			name:     "filter with duplicates at start",
			slice:    []int{1, 1, 2, 3, 4},
			item:     1,
			expected: []int{2, 3, 4},
		},
		{
			name:     "filter with duplicates at end",
			slice:    []int{1, 2, 3, 4, 4},
			item:     4,
			expected: []int{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Filter(tt.slice, tt.item)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Filter() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFilterString(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		item     string
		expected []string
	}{
		{
			name:     "filter string from slice",
			slice:    []string{"apple", "banana", "cherry", "banana"},
			item:     "banana",
			expected: []string{"apple", "cherry"},
		},
		{
			name:     "filter non-existing string",
			slice:    []string{"apple", "banana", "cherry"},
			item:     "orange",
			expected: []string{"apple", "banana", "cherry"},
		},
		{
			name:     "filter empty string",
			slice:    []string{"", "apple", "", "banana"},
			item:     "",
			expected: []string{"apple", "banana"},
		},
		{
			name:     "case sensitive filtering",
			slice:    []string{"Apple", "apple", "APPLE"},
			item:     "apple",
			expected: []string{"Apple", "APPLE"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Filter(tt.slice, tt.item)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Filter() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFilterFloat(t *testing.T) {
	tests := []struct {
		name     string
		slice    []float64
		item     float64
		expected []float64
	}{
		{
			name:     "filter float from slice",
			slice:    []float64{1.1, 2.2, 3.3, 2.2, 4.4},
			item:     2.2,
			expected: []float64{1.1, 3.3, 4.4},
		},
		{
			name:     "filter zero value",
			slice:    []float64{0.0, 1.1, 0.0, 2.2},
			item:     0.0,
			expected: []float64{1.1, 2.2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Filter(tt.slice, tt.item)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Filter() = %v, want %v", result, tt.expected)
			}
		})
	}
}

type CustomComparable struct {
	ID   int
	Name string
}

func TestFilterCustomType(t *testing.T) {
	slice := []CustomComparable{
		{ID: 1, Name: "Alice"},
		{ID: 2, Name: "Bob"},
		{ID: 1, Name: "Alice"},
		{ID: 3, Name: "Charlie"},
	}

	item := CustomComparable{ID: 1, Name: "Alice"}
	expected := []CustomComparable{
		{ID: 2, Name: "Bob"},
		{ID: 3, Name: "Charlie"},
	}

	result := Filter(slice, item)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Filter() with custom type = %v, want %v", result, expected)
	}
}

func TestFilterPreservesCapacity(t *testing.T) {
	// Test that Filter doesn't unnecessarily allocate more memory
	slice := []int{1, 2, 3, 4, 5}
	result := Filter(slice, 6) // Item not in slice

	// Result should be identical to input
	if !reflect.DeepEqual(result, slice) {
		t.Errorf("Filter() should preserve slice when item not found = %v, want %v", result, slice)
	}
}

func TestFilterNilSlice(t *testing.T) {
	// Test with nil slice
	var slice []int
	result := Filter(slice, 1)
	expected := []int{}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Filter() with nil slice = %v, want %v", result, expected)
	}
}
