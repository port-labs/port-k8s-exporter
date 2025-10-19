package jq

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/port-labs/port-k8s-exporter/pkg/config"
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

func TestJqEnvironmentVariablesRestriction(t *testing.T) {
	// Set a test environment variable
	os.Setenv("TEST_ENV_VAR", "test_value")
	defer os.Unsetenv("TEST_ENV_VAR")

	// Test object
	testObj := map[string]interface{}{
		"name":  "test",
		"value": "test_value",
	}

	// Test with environment variables allowed (explicitly enabled)
	originalConfig := config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = true
	result, err := ParseString("env.TEST_ENV_VAR", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "test_value", result)

	// Test with environment variables restricted (default behavior)
	// When restricted, env function returns empty object, so env.VAR returns null
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = false
	result, err = ParseString("env.TEST_ENV_VAR", testObj)
	assert.Error(t, err) // Should error because ParseString expects a string but gets null
	assert.Contains(t, err.Error(), "failed to parse string with jq")

	// Test that regular jq queries still work when environment variables are restricted
	result, err = ParseString(".name", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "test", result)

	// Test ParseInterface with environment variables
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = true
	resultInterface, err := ParseInterface("env.TEST_ENV_VAR", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "test_value", resultInterface)

	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = false
	resultInterface, err = ParseInterface("env.TEST_ENV_VAR", testObj)
	assert.NoError(t, err)         // Should not error, but return null
	assert.Nil(t, resultInterface) // null because env.TEST_ENV_VAR returns null

	// Restore original config
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = originalConfig
}

func TestEnvironmentVariableRestrictionWhenAllowed(t *testing.T) {
	// Set a test environment variable
	os.Setenv("TEST_ENV_VAR", "test_value")
	defer os.Unsetenv("TEST_ENV_VAR")

	testObj := map[string]interface{}{
		"name":  "test",
		"value": "test_value",
	}

	originalConfig := config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ
	defer func() {
		config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = originalConfig
	}()

	// Test all parser functions with environment variables allowed
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = true

	// Test ParseString
	result, err := ParseString("env.TEST_ENV_VAR", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "test_value", result)

	// Test ParseBool
	resultBool, err := ParseBool("env.TEST_ENV_VAR == \"test_value\"", testObj)
	assert.NoError(t, err)
	assert.True(t, resultBool)

	// Test ParseInterface
	resultInterface, err := ParseInterface("env.TEST_ENV_VAR", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "test_value", resultInterface)

	// Test ParseArray
	resultArray, err := ParseArray("[env.TEST_ENV_VAR]", testObj)
	assert.NoError(t, err)
	assert.Equal(t, []interface{}{"test_value"}, resultArray)

	// Test ParseMapInterface
	resultMap, err := ParseMapInterface(map[string]string{"env_var": "env.TEST_ENV_VAR"}, testObj)
	assert.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"env_var": "test_value"}, resultMap)

	// Test ParseMapRecursively
	resultRecursive, err := ParseMapRecursively(map[string]interface{}{"env_var": "env.TEST_ENV_VAR"}, testObj)
	assert.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"env_var": "test_value"}, resultRecursive)
}

func TestRegularJqQueriesWorkWhenEnvVarsRestricted(t *testing.T) {
	testObj := map[string]interface{}{
		"name":  "test",
		"value": "test_value",
		"items": []interface{}{"item1", "item2"},
		"nested": map[string]interface{}{
			"property": "nested_value",
		},
	}

	originalConfig := config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ
	defer func() {
		config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = originalConfig
	}()

	// Test with environment variables restricted
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = false

	// Test ParseString with regular object access
	result, err := ParseString(".name", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "test", result)

	// Test ParseBool with regular object access
	resultBool, err := ParseBool(".name == \"test\"", testObj)
	assert.NoError(t, err)
	assert.True(t, resultBool)

	// Test ParseInterface with regular object access
	resultInterface, err := ParseInterface(".nested.property", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "nested_value", resultInterface)

	// Test ParseArray with regular object access
	resultArray, err := ParseArray(".items", testObj)
	assert.NoError(t, err)
	assert.Equal(t, []interface{}{"item1", "item2"}, resultArray)

	// Test ParseMapInterface with regular object access
	resultMap, err := ParseMapInterface(map[string]string{
		"name":  ".name",
		"value": ".value",
	}, testObj)
	assert.NoError(t, err)
	assert.Equal(t, map[string]interface{}{
		"name":  "test",
		"value": "test_value",
	}, resultMap)

	// Test ParseMapRecursively with regular object access
	resultRecursive, err := ParseMapRecursively(map[string]interface{}{
		"nested": map[string]interface{}{
			"property": ".nested.property",
		},
	}, testObj)
	assert.NoError(t, err)
	assert.Equal(t, map[string]interface{}{
		"nested": map[string]interface{}{
			"property": "nested_value",
		},
	}, resultRecursive)
}

func TestEnvironmentVariableRestrictionComplexQueries(t *testing.T) {
	// Set a test environment variable
	os.Setenv("TEST_ENV_VAR", "test_value")
	defer os.Unsetenv("TEST_ENV_VAR")

	testObj := map[string]interface{}{
		"name":  "test",
		"items": []interface{}{"item1", "item2"},
	}

	originalConfig := config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ
	defer func() {
		config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = originalConfig
	}()

	// Test with environment variables restricted
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = false

	// Test complex queries with env vars that should work but return null
	queries := []struct {
		query    string
		expected interface{}
		desc     string
	}{
		{"env.TEST_ENV_VAR", nil, "simple env var access"},
		{"env.TEST_ENV_VAR // \"default\"", "default", "env var with default fallback"},
		{"if env.TEST_ENV_VAR then .name else \"no_env\" end", "no_env", "conditional with env var"},
		{"select(env.TEST_ENV_VAR) | .name", nil, "select with env var (no results)"},
		{"map(env.TEST_ENV_VAR)", []interface{}{nil, nil}, "map with env var"},
		{"[env.TEST_ENV_VAR, .name]", []interface{}{nil, "test"}, "array with env var and object property"},
	}

	for _, tc := range queries {
		t.Run(tc.desc, func(t *testing.T) {
			result, err := ParseInterface(tc.query, testObj)
			if tc.desc == "select with env var (no results)" {
				// Select with null condition returns no results, causing an error
				assert.Error(t, err, "Query: %s", tc.query)
				assert.Contains(t, err.Error(), "Failed to run jq query", "Query: %s", tc.query)
			} else {
				assert.NoError(t, err, "Query: %s", tc.query)
				assert.Equal(t, tc.expected, result, "Query: %s", tc.query)
			}
		})
	}

	// Test with environment variables allowed
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = true

	// Test the same queries but with actual env var values
	queriesWithEnv := []struct {
		query    string
		expected interface{}
		desc     string
	}{
		{"env.TEST_ENV_VAR", "test_value", "simple env var access"},
		{"env.TEST_ENV_VAR // \"default\"", "test_value", "env var with default fallback"},
		{"if env.TEST_ENV_VAR then .name else \"no_env\" end", "test", "conditional with env var"},
		{"select(env.TEST_ENV_VAR) | .name", "test", "select with env var"},
		{"map(env.TEST_ENV_VAR)", []interface{}{"test_value", "test_value"}, "map with env var"},
		{"[env.TEST_ENV_VAR, .name]", []interface{}{"test_value", "test"}, "array with env var and object property"},
	}

	for _, tc := range queriesWithEnv {
		t.Run(tc.desc+"_with_env", func(t *testing.T) {
			result, err := ParseInterface(tc.query, testObj)
			assert.NoError(t, err, "Query: %s", tc.query)
			assert.Equal(t, tc.expected, result, "Query: %s", tc.query)
		})
	}
}

func TestEnvironmentVariableWhitelist(t *testing.T) {
	// Set test environment variables
	os.Setenv("ALLOWED_VAR1", "value1")
	os.Setenv("ALLOWED_VAR2", "value2")
	os.Setenv("BLOCKED_VAR", "blocked_value")
	defer os.Unsetenv("ALLOWED_VAR1")
	defer os.Unsetenv("ALLOWED_VAR2")
	defer os.Unsetenv("BLOCKED_VAR")

	testObj := map[string]interface{}{
		"name": "test",
	}

	originalConfig := config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ
	originalAllowedVars := config.ApplicationConfig.AllowedEnvironmentVariablesInJQ
	defer func() {
		config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = originalConfig
		config.ApplicationConfig.AllowedEnvironmentVariablesInJQ = originalAllowedVars
	}()

	// Test with environment variables restricted but with whitelist
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = false
	config.ApplicationConfig.AllowedEnvironmentVariablesInJQ = []string{"ALLOWED_VAR1", "ALLOWED_VAR2"}

	// Test that allowed variables are accessible
	result, err := ParseInterface("env.ALLOWED_VAR1", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "value1", result)

	result, err = ParseInterface("env.ALLOWED_VAR2", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "value2", result)

	// Test that blocked variables are not accessible
	result, err = ParseInterface("env.BLOCKED_VAR", testObj)
	assert.NoError(t, err)
	assert.Nil(t, result) // Should be null because it's not in the whitelist

	// Test that non-existent variables are not accessible
	result, err = ParseInterface("env.NON_EXISTENT_VAR", testObj)
	assert.NoError(t, err)
	assert.Nil(t, result) // Should be null because it doesn't exist

	// Test that the env object only contains allowed variables
	result, err = ParseInterface("env", testObj)
	assert.NoError(t, err)
	envMap, ok := result.(map[string]interface{})
	assert.True(t, ok)
	assert.Contains(t, envMap, "ALLOWED_VAR1")
	assert.Contains(t, envMap, "ALLOWED_VAR2")
	assert.NotContains(t, envMap, "BLOCKED_VAR")
	assert.Equal(t, "value1", envMap["ALLOWED_VAR1"])
	assert.Equal(t, "value2", envMap["ALLOWED_VAR2"])
}

func TestEnvironmentVariableWhitelistEmpty(t *testing.T) {
	// Set test environment variables
	os.Setenv("TEST_VAR", "test_value")
	defer os.Unsetenv("TEST_VAR")

	testObj := map[string]interface{}{
		"name": "test",
	}

	originalConfig := config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ
	originalAllowedVars := config.ApplicationConfig.AllowedEnvironmentVariablesInJQ
	defer func() {
		config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = originalConfig
		config.ApplicationConfig.AllowedEnvironmentVariablesInJQ = originalAllowedVars
	}()

	// Test with environment variables restricted and empty whitelist
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = false
	config.ApplicationConfig.AllowedEnvironmentVariablesInJQ = []string{}

	// Test that no environment variables are accessible
	result, err := ParseInterface("env.TEST_VAR", testObj)
	assert.NoError(t, err)
	assert.Nil(t, result) // Should be null because whitelist is empty

	// Test that the env object is empty
	result, err = ParseInterface("env", testObj)
	assert.NoError(t, err)
	envMap, ok := result.(map[string]interface{})
	assert.True(t, ok)
	assert.Empty(t, envMap) // Should be empty because whitelist is empty
}

func TestEnvironmentVariableWhitelistComplexQueries(t *testing.T) {
	// Set test environment variables
	os.Setenv("CLUSTER_NAME", "test-cluster")
	os.Setenv("ENVIRONMENT", "production")
	os.Setenv("SECRET_VAR", "secret_value")
	defer os.Unsetenv("CLUSTER_NAME")
	defer os.Unsetenv("ENVIRONMENT")
	defer os.Unsetenv("SECRET_VAR")

	testObj := map[string]interface{}{
		"name":      "test-app",
		"namespace": "default",
	}

	originalConfig := config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ
	originalAllowedVars := config.ApplicationConfig.AllowedEnvironmentVariablesInJQ
	defer func() {
		config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = originalConfig
		config.ApplicationConfig.AllowedEnvironmentVariablesInJQ = originalAllowedVars
	}()

	// Test with environment variables restricted but with whitelist
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = false
	config.ApplicationConfig.AllowedEnvironmentVariablesInJQ = []string{"CLUSTER_NAME", "ENVIRONMENT"}

	// Test complex queries with allowed variables
	queries := []struct {
		query    string
		expected interface{}
		desc     string
	}{
		{"env.CLUSTER_NAME", "test-cluster", "simple allowed env var access"},
		{"env.CLUSTER_NAME // \"default-cluster\"", "test-cluster", "allowed env var with fallback"},
		{"if env.CLUSTER_NAME then .name + \"-\" + env.CLUSTER_NAME else .name end", "test-app-test-cluster", "conditional with allowed env var"},
		{"[env.CLUSTER_NAME, env.ENVIRONMENT, .name]", []interface{}{"test-cluster", "production", "test-app"}, "array with allowed env vars and object property"},
		{"env.SECRET_VAR", nil, "blocked env var should return null"},
		{"env.SECRET_VAR // \"default-secret\"", "default-secret", "blocked env var with fallback"},
	}

	for _, tc := range queries {
		t.Run(tc.desc, func(t *testing.T) {
			result, err := ParseInterface(tc.query, testObj)
			assert.NoError(t, err, "Query: %s", tc.query)
			assert.Equal(t, tc.expected, result, "Query: %s", tc.query)
		})
	}
}

func TestEnvironmentVariablePrefixWhitelist(t *testing.T) {
	// Set test environment variables
	os.Setenv("PORT_CLIENT_ID", "client123")
	os.Setenv("PORT_BASE_URL", "https://api.getport.io")
	os.Setenv("PORT_STATE_KEY", "my-state")
	os.Setenv("KUBE_NAMESPACE", "default")
	os.Setenv("KUBE_CLUSTER", "minikube")
	os.Setenv("SECRET_KEY", "secret123")
	os.Setenv("DATABASE_URL", "postgres://localhost")
	defer os.Unsetenv("PORT_CLIENT_ID")
	defer os.Unsetenv("PORT_BASE_URL")
	defer os.Unsetenv("PORT_STATE_KEY")
	defer os.Unsetenv("KUBE_NAMESPACE")
	defer os.Unsetenv("KUBE_CLUSTER")
	defer os.Unsetenv("SECRET_KEY")
	defer os.Unsetenv("DATABASE_URL")

	testObj := map[string]interface{}{
		"name": "test",
	}

	originalConfig := config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ
	originalAllowedVars := config.ApplicationConfig.AllowedEnvironmentVariablesInJQ
	defer func() {
		config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = originalConfig
		config.ApplicationConfig.AllowedEnvironmentVariablesInJQ = originalAllowedVars
	}()

	// Test with environment variables restricted but with prefix whitelist
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = false
	config.ApplicationConfig.AllowedEnvironmentVariablesInJQ = []string{"PORT_*", "KUBE_*"}

	// Test that PORT_* variables are accessible
	result, err := ParseInterface("env.PORT_CLIENT_ID", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "client123", result)

	result, err = ParseInterface("env.PORT_BASE_URL", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "https://api.getport.io", result)

	result, err = ParseInterface("env.PORT_STATE_KEY", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "my-state", result)

	// Test that KUBE_* variables are accessible
	result, err = ParseInterface("env.KUBE_NAMESPACE", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "default", result)

	result, err = ParseInterface("env.KUBE_CLUSTER", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "minikube", result)

	// Test that non-prefixed variables are blocked
	result, err = ParseInterface("env.SECRET_KEY", testObj)
	assert.NoError(t, err)
	assert.Nil(t, result) // Should be null because it doesn't match any prefix

	result, err = ParseInterface("env.DATABASE_URL", testObj)
	assert.NoError(t, err)
	assert.Nil(t, result) // Should be null because it doesn't match any prefix

	// Test that the env object only contains allowed variables
	result, err = ParseInterface("env", testObj)
	assert.NoError(t, err)
	envMap, ok := result.(map[string]interface{})
	assert.True(t, ok)

	// Should contain PORT_* variables
	assert.Contains(t, envMap, "PORT_CLIENT_ID")
	assert.Contains(t, envMap, "PORT_BASE_URL")
	assert.Contains(t, envMap, "PORT_STATE_KEY")

	// Should contain KUBE_* variables
	assert.Contains(t, envMap, "KUBE_NAMESPACE")
	assert.Contains(t, envMap, "KUBE_CLUSTER")

	// Should NOT contain non-prefixed variables
	assert.NotContains(t, envMap, "SECRET_KEY")
	assert.NotContains(t, envMap, "DATABASE_URL")

	// Verify values
	assert.Equal(t, "client123", envMap["PORT_CLIENT_ID"])
	assert.Equal(t, "https://api.getport.io", envMap["PORT_BASE_URL"])
	assert.Equal(t, "my-state", envMap["PORT_STATE_KEY"])
	assert.Equal(t, "default", envMap["KUBE_NAMESPACE"])
	assert.Equal(t, "minikube", envMap["KUBE_CLUSTER"])
}

func TestEnvironmentVariableMixedWhitelist(t *testing.T) {
	// Set test environment variables
	os.Setenv("PORT_CLIENT_ID", "client123")
	os.Setenv("PORT_BASE_URL", "https://api.getport.io")
	os.Setenv("CLUSTER_NAME", "my-cluster")
	os.Setenv("ENVIRONMENT", "production")
	os.Setenv("SECRET_KEY", "secret123")
	os.Setenv("DATABASE_URL", "postgres://localhost")
	defer os.Unsetenv("PORT_CLIENT_ID")
	defer os.Unsetenv("PORT_BASE_URL")
	defer os.Unsetenv("CLUSTER_NAME")
	defer os.Unsetenv("ENVIRONMENT")
	defer os.Unsetenv("SECRET_KEY")
	defer os.Unsetenv("DATABASE_URL")

	testObj := map[string]interface{}{
		"name": "test",
	}

	originalConfig := config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ
	originalAllowedVars := config.ApplicationConfig.AllowedEnvironmentVariablesInJQ
	defer func() {
		config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = originalConfig
		config.ApplicationConfig.AllowedEnvironmentVariablesInJQ = originalAllowedVars
	}()

	// Test with mixed exact matches and prefixes
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = false
	config.ApplicationConfig.AllowedEnvironmentVariablesInJQ = []string{"PORT_*", "CLUSTER_NAME", "ENVIRONMENT"}

	// Test that PORT_* variables are accessible
	result, err := ParseInterface("env.PORT_CLIENT_ID", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "client123", result)

	result, err = ParseInterface("env.PORT_BASE_URL", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "https://api.getport.io", result)

	// Test that exact match variables are accessible
	result, err = ParseInterface("env.CLUSTER_NAME", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "my-cluster", result)

	result, err = ParseInterface("env.ENVIRONMENT", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "production", result)

	// Test that non-allowed variables are blocked
	result, err = ParseInterface("env.SECRET_KEY", testObj)
	assert.NoError(t, err)
	assert.Nil(t, result) // Should be null because it's not allowed

	result, err = ParseInterface("env.DATABASE_URL", testObj)
	assert.NoError(t, err)
	assert.Nil(t, result) // Should be null because it's not allowed

	// Test that the env object contains both exact matches and prefix matches
	result, err = ParseInterface("env", testObj)
	assert.NoError(t, err)
	envMap, ok := result.(map[string]interface{})
	assert.True(t, ok)

	// Should contain PORT_* variables
	assert.Contains(t, envMap, "PORT_CLIENT_ID")
	assert.Contains(t, envMap, "PORT_BASE_URL")

	// Should contain exact match variables
	assert.Contains(t, envMap, "CLUSTER_NAME")
	assert.Contains(t, envMap, "ENVIRONMENT")

	// Should NOT contain non-allowed variables
	assert.NotContains(t, envMap, "SECRET_KEY")
	assert.NotContains(t, envMap, "DATABASE_URL")
}

func TestEnvironmentVariablePrefixEdgeCases(t *testing.T) {
	// Set test environment variables
	os.Setenv("PORT", "port-value")
	os.Setenv("PORT_", "port-underscore-value")
	os.Setenv("PORT_CLIENT_ID", "client123")
	os.Setenv("PORT_CLIENT_SECRET", "secret456")
	os.Setenv("NOT_PORT", "not-port-value")
	os.Setenv("PORT_NOT", "port-not-value")
	defer os.Unsetenv("PORT")
	defer os.Unsetenv("PORT_")
	defer os.Unsetenv("PORT_CLIENT_ID")
	defer os.Unsetenv("PORT_CLIENT_SECRET")
	defer os.Unsetenv("NOT_PORT")
	defer os.Unsetenv("PORT_NOT")

	testObj := map[string]interface{}{
		"name": "test",
	}

	originalConfig := config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ
	originalAllowedVars := config.ApplicationConfig.AllowedEnvironmentVariablesInJQ
	defer func() {
		config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = originalConfig
		config.ApplicationConfig.AllowedEnvironmentVariablesInJQ = originalAllowedVars
	}()

	// Test with PORT_* prefix
	config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ = false
	config.ApplicationConfig.AllowedEnvironmentVariablesInJQ = []string{"PORT_*"}

	// Test exact prefix match (PORT_)
	result, err := ParseInterface("env.PORT_", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "port-underscore-value", result)

	// Test variables that start with PORT_
	result, err = ParseInterface("env.PORT_CLIENT_ID", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "client123", result)

	result, err = ParseInterface("env.PORT_CLIENT_SECRET", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "secret456", result)

	// Test variables that don't start with PORT_
	result, err = ParseInterface("env.PORT", testObj)
	assert.NoError(t, err)
	assert.Nil(t, result) // Should be null because "PORT" doesn't start with "PORT_"

	result, err = ParseInterface("env.NOT_PORT", testObj)
	assert.NoError(t, err)
	assert.Nil(t, result) // Should be null because it doesn't start with "PORT_"

	result, err = ParseInterface("env.PORT_NOT", testObj)
	assert.NoError(t, err)
	assert.Equal(t, "port-not-value", result) // Should match because "PORT_NOT" starts with "PORT_"

	// Test that the env object only contains variables that start with PORT_
	result, err = ParseInterface("env", testObj)
	assert.NoError(t, err)
	envMap, ok := result.(map[string]interface{})
	assert.True(t, ok)

	// Should contain variables that start with PORT_
	assert.Contains(t, envMap, "PORT_")
	assert.Contains(t, envMap, "PORT_CLIENT_ID")
	assert.Contains(t, envMap, "PORT_CLIENT_SECRET")
	assert.Contains(t, envMap, "PORT_NOT") // PORT_NOT starts with PORT_

	// Should NOT contain variables that don't start with PORT_
	assert.NotContains(t, envMap, "PORT")
	assert.NotContains(t, envMap, "NOT_PORT")
}
