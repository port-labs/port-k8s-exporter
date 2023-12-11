package goutils

import (
	"fmt"
	"os"
	"strconv"
)

func GetEnvOrDefault(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func GetBooleanEnvOrDefault(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value == "true"
}

func GetIntEnvOrDefault(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	result, err := strconv.Atoi(value)
	if err != nil {
		fmt.Printf("Using default value "+strconv.Itoa(defaultValue)+" for "+key+". error parsing env variable %s: %s", key, err.Error())
		return defaultValue
	}
	return result
}
