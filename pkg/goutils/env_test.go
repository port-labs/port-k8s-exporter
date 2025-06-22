package goutils

import (
	"os"
	"testing"
)

func TestGetStringEnvOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		setEnv       bool
		expected     string
	}{
		{
			name:         "env var is set",
			key:          "TEST_STRING_SET",
			defaultValue: "default",
			envValue:     "custom_value",
			setEnv:       true,
			expected:     "custom_value",
		},
		{
			name:         "env var is not set",
			key:          "TEST_STRING_NOT_SET",
			defaultValue: "default",
			setEnv:       false,
			expected:     "default",
		},
		{
			name:         "env var is empty string",
			key:          "TEST_STRING_EMPTY",
			defaultValue: "default",
			envValue:     "",
			setEnv:       true,
			expected:     "default",
		},
		{
			name:         "empty default value",
			key:          "TEST_STRING_EMPTY_DEFAULT",
			defaultValue: "",
			envValue:     "value",
			setEnv:       true,
			expected:     "value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up env var before test
			os.Unsetenv(tt.key)

			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			result := GetStringEnvOrDefault(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("GetStringEnvOrDefault() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetUintEnvOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue uint64
		envValue     string
		setEnv       bool
		expected     uint64
	}{
		{
			name:         "valid uint env var",
			key:          "TEST_UINT_VALID",
			defaultValue: 42,
			envValue:     "123",
			setEnv:       true,
			expected:     123,
		},
		{
			name:         "env var not set",
			key:          "TEST_UINT_NOT_SET",
			defaultValue: 42,
			setEnv:       false,
			expected:     42,
		},
		{
			name:         "empty env var",
			key:          "TEST_UINT_EMPTY",
			defaultValue: 42,
			envValue:     "",
			setEnv:       true,
			expected:     42,
		},
		{
			name:         "invalid uint env var",
			key:          "TEST_UINT_INVALID",
			defaultValue: 42,
			envValue:     "not_a_number",
			setEnv:       true,
			expected:     42,
		},
		{
			name:         "negative number",
			key:          "TEST_UINT_NEGATIVE",
			defaultValue: 42,
			envValue:     "-123",
			setEnv:       true,
			expected:     42,
		},
		{
			name:         "zero value",
			key:          "TEST_UINT_ZERO",
			defaultValue: 42,
			envValue:     "0",
			setEnv:       true,
			expected:     0,
		},
		{
			name:         "large uint value",
			key:          "TEST_UINT_LARGE",
			defaultValue: 42,
			envValue:     "4294967295", // max uint32
			setEnv:       true,
			expected:     4294967295,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up env var before test
			os.Unsetenv(tt.key)

			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			result := GetUintEnvOrDefault(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("GetUintEnvOrDefault() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetBoolEnvOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue bool
		envValue     string
		setEnv       bool
		expected     bool
	}{
		{
			name:         "true string",
			key:          "TEST_BOOL_TRUE",
			defaultValue: false,
			envValue:     "true",
			setEnv:       true,
			expected:     true,
		},
		{
			name:         "false string",
			key:          "TEST_BOOL_FALSE",
			defaultValue: true,
			envValue:     "false",
			setEnv:       true,
			expected:     false,
		},
		{
			name:         "1 string",
			key:          "TEST_BOOL_ONE",
			defaultValue: false,
			envValue:     "1",
			setEnv:       true,
			expected:     true,
		},
		{
			name:         "0 string",
			key:          "TEST_BOOL_ZERO",
			defaultValue: true,
			envValue:     "0",
			setEnv:       true,
			expected:     false,
		},
		{
			name:         "env var not set",
			key:          "TEST_BOOL_NOT_SET",
			defaultValue: true,
			setEnv:       false,
			expected:     true,
		},
		{
			name:         "empty env var",
			key:          "TEST_BOOL_EMPTY",
			defaultValue: true,
			envValue:     "",
			setEnv:       true,
			expected:     true,
		},
		{
			name:         "invalid bool env var",
			key:          "TEST_BOOL_INVALID",
			defaultValue: false,
			envValue:     "maybe",
			setEnv:       true,
			expected:     false,
		},
		{
			name:         "case insensitive true",
			key:          "TEST_BOOL_TRUE_CASE",
			defaultValue: false,
			envValue:     "TRUE",
			setEnv:       true,
			expected:     true,
		},
		{
			name:         "case insensitive false",
			key:          "TEST_BOOL_FALSE_CASE",
			defaultValue: true,
			envValue:     "FALSE",
			setEnv:       true,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up env var before test
			os.Unsetenv(tt.key)

			if tt.setEnv {
				os.Setenv(tt.key, tt.envValue)
				defer os.Unsetenv(tt.key)
			}

			result := GetBoolEnvOrDefault(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("GetBoolEnvOrDefault() = %v, want %v", result, tt.expected)
			}
		})
	}
}
