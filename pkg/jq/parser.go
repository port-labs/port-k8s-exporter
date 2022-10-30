package jq

import (
	"fmt"
	"github.com/itchyny/gojq"
	"strings"
)

func runJQQuery(jqQuery string, obj interface{}) (interface{}, error) {
	query, err := gojq.Parse(jqQuery)
	if err != nil {
		return nil, err
	}

	queryRes, ok := query.Run(obj).Next()

	if !ok {
		return nil, fmt.Errorf("query should return at least one value")
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
		return false, fmt.Errorf("failed to parse bool: %#v", queryRes)
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
		return "", fmt.Errorf("failed to parse string: %#v", queryRes)
	}

	return strings.Trim(str, "\""), nil
}

func ParseMapInterface(jqQueries map[string]string, obj interface{}) (map[string]interface{}, error) {
	mapInterface := make(map[string]interface{}, len(jqQueries))

	for key, jqQuery := range jqQueries {
		queryRes, err := runJQQuery(jqQuery, obj)
		if err != nil {
			return nil, err
		}

		mapInterface[key] = queryRes
	}

	return mapInterface, nil
}

func ParseMapString(jqQueries map[string]string, obj interface{}) (map[string]string, error) {
	mapString := make(map[string]string, len(jqQueries))

	for key, jqQuery := range jqQueries {
		queryRes, err := ParseString(jqQuery, obj)
		if err != nil {
			return nil, err
		}

		mapString[key] = queryRes
	}

	return mapString, nil
}
