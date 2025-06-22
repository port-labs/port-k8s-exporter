package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfiguration_FileNotFound(t *testing.T) {
	// Setup
	originalConfigFilePath := ApplicationConfig.ConfigFilePath
	defer func() {
		ApplicationConfig.ConfigFilePath = originalConfigFilePath
	}()

	ApplicationConfig.ConfigFilePath = "non-existent-file.yaml"
	ApplicationConfig.StateKey = "test-state-key"
	ApplicationConfig.EventListenerType = "POLLING"
	ApplicationConfig.CreateDefaultResources = true
	ApplicationConfig.CreatePortResourcesOrigin = port.CreatePortResourcesOriginPort
	ApplicationConfig.ResyncInterval = 300
	ApplicationConfig.OverwriteConfigurationOnRestart = false
	ApplicationConfig.CreateMissingRelatedEntities = false
	ApplicationConfig.DeleteDependents = false

	// Test
	config, err := NewConfiguration()

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "test-state-key", config.StateKey)
	assert.Equal(t, "POLLING", config.EventListenerType)
	assert.Equal(t, true, config.CreateDefaultResources)
	assert.Equal(t, port.CreatePortResourcesOriginPort, config.CreatePortResourcesOrigin)
	assert.Equal(t, uint(300), config.ResyncInterval)
	assert.Equal(t, false, config.OverwriteConfigurationOnRestart)
	assert.Equal(t, false, config.CreateMissingRelatedEntities)
	assert.Equal(t, false, config.DeleteDependents)
}

func TestNewConfiguration_ValidConfigFile(t *testing.T) {
	// Setup - Create a temporary config file
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "test-config.yaml")

	configContent := `
stateKey: "yaml-state-key"
eventListenerType: "KAFKA"
resyncInterval: 600
createDefaultResources: false
createPortResourcesOrigin: "K8S"
overwriteConfigurationOnRestart: true
deleteDependents: true
createMissingRelatedEntities: true
resources:
  - kind: "Deployment"
    selector:
      query: "metadata.namespace = 'default'"
    port:
      entity:
        mappings:
          - identifier: ".metadata.name"
            title: ".metadata.name"
            blueprint: "deployment"
            properties:
              namespace: ".metadata.namespace"
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Backup original config
	originalConfigFilePath := ApplicationConfig.ConfigFilePath
	defer func() {
		ApplicationConfig.ConfigFilePath = originalConfigFilePath
	}()

	ApplicationConfig.ConfigFilePath = configFile
	ApplicationConfig.StateKey = "app-state-key" // This should be overridden by YAML

	// Test
	config, err := NewConfiguration()

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "yaml-state-key", config.StateKey) // Should come from YAML
	assert.Equal(t, "KAFKA", config.EventListenerType)
	assert.Equal(t, uint(600), config.ResyncInterval)
	assert.Equal(t, false, config.CreateDefaultResources)
	assert.Equal(t, port.CreatePortResourcesOriginK8S, config.CreatePortResourcesOrigin)
	assert.Equal(t, true, config.OverwriteConfigurationOnRestart)
	assert.Equal(t, true, config.DeleteDependents)
	assert.Equal(t, true, config.CreateMissingRelatedEntities)
	assert.Len(t, config.Resources, 1)
	assert.Equal(t, "Deployment", config.Resources[0].Kind)
}

func TestNewConfiguration_InvalidYAML(t *testing.T) {
	// Setup - Create a temporary config file with invalid YAML
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "invalid-config.yaml")

	invalidYAMLContent := `
stateKey: "test"
invalidYAML: [
  missing: closing bracket
resources:
  - kind: unclosed
`

	err := os.WriteFile(configFile, []byte(invalidYAMLContent), 0644)
	require.NoError(t, err)

	// Backup original config
	originalConfigFilePath := ApplicationConfig.ConfigFilePath
	defer func() {
		ApplicationConfig.ConfigFilePath = originalConfigFilePath
	}()

	ApplicationConfig.ConfigFilePath = configFile

	// Test
	config, err := NewConfiguration()

	// Assert
	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "failed loading configuration")
}

func TestNewConfiguration_StateKeyLowercase(t *testing.T) {
	tests := []struct {
		name     string
		stateKey string
		expected string
	}{
		{
			name:     "uppercase state key",
			stateKey: "UPPERCASE-STATE-KEY",
			expected: "uppercase-state-key",
		},
		{
			name:     "mixed case state key",
			stateKey: "MiXeD-CaSe-StAtE-KeY",
			expected: "mixed-case-state-key",
		},
		{
			name:     "already lowercase",
			stateKey: "already-lowercase",
			expected: "already-lowercase",
		},
		{
			name:     "with numbers and special chars",
			stateKey: "Test123_Key-With.Special",
			expected: "test123_key-with.special",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup - Create a temporary config file
			tempDir := t.TempDir()
			configFile := filepath.Join(tempDir, "test-config.yaml")

			configContent := `
stateKey: "` + tt.stateKey + `"
eventListenerType: "KAFKA"
resyncInterval: 600
createDefaultResources: false
createPortResourcesOrigin: "K8S"
overwriteConfigurationOnRestart: true
deleteDependents: true
createMissingRelatedEntities: true
resources:
  - kind: "Deployment"
    selector:
      query: "metadata.namespace = 'default'"
    port:
      entity:
        mappings:
          - identifier: ".metadata.name"
            title: ".metadata.name"
            blueprint: "deployment"
            properties:
              namespace: ".metadata.namespace"
`

			err := os.WriteFile(configFile, []byte(configContent), 0644)
			require.NoError(t, err)

			// Backup original config
			originalConfigFilePath := ApplicationConfig.ConfigFilePath
			originalStateKey := ApplicationConfig.StateKey
			defer func() {
				ApplicationConfig.ConfigFilePath = originalConfigFilePath
				ApplicationConfig.StateKey = originalStateKey
			}()
			ApplicationConfig.ConfigFilePath = configFile

			// Test
			config, err := NewConfiguration()

			// Assert
			assert.NoError(t, err)
			assert.NotNil(t, config)
			assert.Equal(t, tt.expected, config.StateKey)
		})
	}
}

func TestNewConfiguration_StateKeyFromYAMLLowercase(t *testing.T) {
	// Setup - Create a temporary config file with uppercase state key
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "test-config.yaml")

	configContent := `stateKey: "YAML-UPPERCASE-KEY"`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Backup original config
	originalConfigFilePath := ApplicationConfig.ConfigFilePath
	defer func() {
		ApplicationConfig.ConfigFilePath = originalConfigFilePath
	}()

	ApplicationConfig.ConfigFilePath = configFile

	// Test
	config, err := NewConfiguration()

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "yaml-uppercase-key", config.StateKey)
}

func TestNewConfiguration_EmptyFile(t *testing.T) {
	// Setup - Create an empty config file
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "empty-config.yaml")

	err := os.WriteFile(configFile, []byte(""), 0644)
	require.NoError(t, err)

	// Backup original config
	originalConfigFilePath := ApplicationConfig.ConfigFilePath
	defer func() {
		ApplicationConfig.ConfigFilePath = originalConfigFilePath
	}()

	ApplicationConfig.ConfigFilePath = configFile
	ApplicationConfig.StateKey = "test-state-key"

	// Test
	config, err := NewConfiguration()

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "test-state-key", config.StateKey)
}

func TestNewConfiguration_PartialConfigFile(t *testing.T) {
	// Setup - Create a config file with only some fields
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "partial-config.yaml")

	configContent := `
stateKey: "partial-config-key"
resyncInterval: 1200
# Only these two fields are set, others should use defaults from ApplicationConfig
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	require.NoError(t, err)

	// Backup original config
	originalConfigFilePath := ApplicationConfig.ConfigFilePath
	defer func() {
		ApplicationConfig.ConfigFilePath = originalConfigFilePath
	}()

	ApplicationConfig.ConfigFilePath = configFile
	ApplicationConfig.EventListenerType = "POLLING"
	ApplicationConfig.CreateDefaultResources = true
	ApplicationConfig.CreatePortResourcesOrigin = port.CreatePortResourcesOriginPort

	// Test
	config, err := NewConfiguration()

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "partial-config-key", config.StateKey)
	assert.Equal(t, uint(1200), config.ResyncInterval)
	// These should come from ApplicationConfig defaults since they're not in YAML
	assert.Equal(t, "POLLING", config.EventListenerType)
	assert.Equal(t, true, config.CreateDefaultResources)
	assert.Equal(t, port.CreatePortResourcesOriginPort, config.CreatePortResourcesOrigin)
}

// TestInit tests the Init function indirectly by checking if flags are properly set
func TestInit_FlagsAreSet(t *testing.T) {
	// This test is more of an integration test to ensure Init() doesn't panic
	// and sets up the expected global variables

	// Save original values
	originalKafkaConfig := *KafkaConfig
	originalPollingListenerRate := PollingListenerRate
	originalApplicationConfig := *ApplicationConfig

	// Reset to zero values
	*KafkaConfig = KafkaConfiguration{}
	PollingListenerRate = 0
	*ApplicationConfig = ApplicationConfiguration{}

	defer func() {
		// Restore original values
		*KafkaConfig = originalKafkaConfig
		PollingListenerRate = originalPollingListenerRate
		*ApplicationConfig = originalApplicationConfig
	}()

	// Test that Init doesn't panic
	assert.NotPanics(t, func() {
		Init()
	})

	// Verify that some default values are set (these come from the flag defaults)
	// Note: The actual values depend on environment variables, so we just check they're not empty/zero
	assert.NotEmpty(t, ApplicationConfig.PortBaseURL)
	assert.NotZero(t, PollingListenerRate)
}
