package jq

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/port-labs/port-k8s-exporter/pkg/config"
	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/logger"
)

func runJQQuery(jqQuery string, obj interface{}) (interface{}, error) {
	query, err := gojq.Parse(jqQuery)
	if err != nil {
		logger.Warningf("failed to parse jq query: %s", jqQuery)
		return nil, err
	}
	code, err := gojq.Compile(
		query,
		gojq.WithEnvironLoader(func() []string {
			envQuery := fmt.Sprintf("def modified_env: [%s | .[] | split(\"=\")] | map({\"key\": .[0], \"value\": .[1]}) | from_entries; def patterns: %s | map(\"(\" + . + \")\") | join(\"|\"); if %t then modified_env else if ((patterns | length) > 0) then modified_env | with_entries(select(.key | test(patterns))) else {} end end | to_entries | map([.key,.value]) | [ .[] | join(\"=\")]", getSerializedEnvironmentVariables(), getAllowedEnvironmentVariables(), config.ApplicationConfig.AllowAllEnvironmentVariablesInJQ)
			parsedEnvQuery, err := gojq.Parse(envQuery)
			if err != nil {
				logger.Warningf("failed to parse environment variables jq query: %s", err)
				return os.Environ()
			}
			env, ok := parsedEnvQuery.Run(map[string]any{}).Next()
			if !ok {
				return os.Environ()
			}
			if err, ok := env.(error); ok {
				logger.Warningf("failed to run environment variables jq query: %s", err)
				return os.Environ()
			}
			if result, ok := env.([]interface{}); ok {
				resultStrings := make([]string, len(result))
				for i, v := range result {
					resultStrings[i] = v.(string)
				}
				return resultStrings
			}
			return os.Environ()
		}),
	)
	if err != nil {
		logger.Warningf("failed to compile jq query: %s", jqQuery)
		return nil, err
	}
	queryRes, ok := code.Run(obj).Next()

	if !ok {
		logger.Errorw(fmt.Sprintf("Failed to run jq query. Query: %s, Object: %#v", jqQuery, obj), "jqQuery", jqQuery, "obj", obj)
		return nil, fmt.Errorf("Failed to run jq query")
	}

	err, ok = queryRes.(error)
	if ok {
		return nil, err
	}

	return queryRes, nil
}

func ParseBool(jqQuery string, obj interface{}) (bool, error) {
	queryRes, err := runJQQuery(jqQuery, obj)
	if err != nil {
		return false, err
	}

	boolean, ok := queryRes.(bool)
	if !ok {
		logger.Errorw(fmt.Sprintf("bool result expected from query: '%#v', but got: %#v", jqQuery, queryRes), "jqQuery", jqQuery, "queryRes", queryRes)
		return false, fmt.Errorf("failed to parse bool")
	}

	return boolean, nil
}

func ParseString(jqQuery string, obj interface{}) (string, error) {
	queryRes, err := runJQQuery(jqQuery, obj)
	if err != nil {
		return "", err
	}

	str, ok := queryRes.(string)
	if !ok {
		logger.Errorw(fmt.Sprintf("string result expected from query: '%#v', but got: %#v", jqQuery, queryRes), "jqQuery", jqQuery, "queryRes", queryRes)
		return "", fmt.Errorf("failed to parse string with jq")
	}

	return strings.Trim(str, "\""), nil
}

func ParseInterface(jqQuery string, obj interface{}) (interface{}, error) {
	queryRes, err := runJQQuery(jqQuery, obj)
	if err != nil {
		return "", err
	}

	return queryRes, nil
}

func ParseArray(jqQuery string, obj interface{}) ([]interface{}, error) {
	queryRes, err := runJQQuery(jqQuery, obj)

	if err != nil {
		return nil, err
	}

	items, ok := queryRes.([]interface{})
	if !ok {
		logger.Errorw(fmt.Sprintf("array result expected from query: '%#v', but got: %#v", jqQuery, queryRes), "jqQuery", jqQuery, "queryRes", queryRes)
		return nil, fmt.Errorf("failed to parse array")
	}

	return items, nil
}

func ParseMapInterface(jqQueries map[string]string, obj interface{}) (map[string]interface{}, error) {
	mapInterface := make(map[string]interface{}, len(jqQueries))

	for key, jqQuery := range jqQueries {
		queryRes, err := ParseInterface(jqQuery, obj)
		if err != nil {
			logger.Errorw(fmt.Sprintf("error parsing property %s. Error: %s", key, err.Error()), "key", key, "error", err, "jqQuery", jqQuery, "obj", obj)
			return nil, err
		}

		if key != "*" {
			mapInterface[key] = queryRes
		} else {
			if _, ok := queryRes.(map[string]interface{}); ok {
				mapInterface = goutils.MergeMaps(mapInterface, queryRes.(map[string]interface{}))
			} else {
				mapInterface[key] = queryRes
			}
		}

	}

	return mapInterface, nil
}

func ParseMapRecursively(jqQueries map[string]interface{}, obj interface{}) (map[string]interface{}, error) {
	mapInterface := make(map[string]interface{}, len(jqQueries))

	for key, jqQuery := range jqQueries {

		if reflect.TypeOf(jqQuery).Kind() == reflect.String {
			queryRes, err := ParseMapInterface(map[string]string{key: jqQuery.(string)}, obj)
			if err != nil {
				return nil, err
			}
			mapInterface = goutils.MergeMaps(mapInterface, queryRes)
		} else if reflect.TypeOf(jqQuery).Kind() == reflect.Map {
			for mapKey, mapValue := range jqQuery.(map[string]interface{}) {
				queryRes, err := ParseMapRecursively(map[string]interface{}{mapKey: mapValue}, obj)
				if err != nil {
					return nil, err
				}
				for queryKey, queryVal := range queryRes {
					if mapInterface[key] == nil {
						mapInterface[key] = make(map[string]interface{})
					}
					mapInterface[key].(map[string]interface{})[queryKey] = queryVal
				}
			}
		} else if reflect.TypeOf(jqQuery).Kind() == reflect.Slice {
			jqArrayValue := reflect.ValueOf(jqQuery)
			relations := make([]interface{}, jqArrayValue.Len())
			for i := 0; i < jqArrayValue.Len(); i++ {
				relation, err := ParseMapRecursively(map[string]interface{}{key: jqArrayValue.Index(i).Interface()}, obj)
				if err != nil {
					return nil, err
				}
				relations[i] = relation[key]
			}
			mapInterface[key] = relations
		} else {
			return nil, fmt.Errorf("invalid jq query type '%T'", jqQuery)
		}
	}

	return mapInterface, nil
}

func getAllowedEnvironmentVariables() string {
	return getSerializedVariablesArray(config.ApplicationConfig.AllowedEnvironmentVariablesInJQ, "failed to marshal allowed environment variables")
}

func getSerializedEnvironmentVariables() string {
	return getSerializedVariablesArray(os.Environ(), "failed to marshal environment variables")
}

func getSerializedVariablesArray(input []string, errorMessage string) string {
	escapedInput := make([]string, len(input))
	for i, v := range input {
		// Escape backslashes first, then double quotes
		escaped := strings.ReplaceAll(v, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		escapedInput[i] = escaped
	}
	jsonBytes, err := json.Marshal(escapedInput)
	if err != nil {
		logger.Warningf("%s: %v", errorMessage, err)
		return "[]"
	}

	return string(jsonBytes)
}
