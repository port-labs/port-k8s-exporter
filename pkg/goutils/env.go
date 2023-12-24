package goutils

import (
	"fmt"
	"os"
	"strconv"
)

func GetStringEnvOrDefault(key string, defaultValue string) string {
	// Try to get from .env file
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func GetUintEnvOrDefault(key string, defaultValue uint64) uint64 {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	result, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		fmt.Printf("Using default value "+strconv.FormatUint(uint64(defaultValue), 10)+" for "+key+". error parsing env variable %s: %s", key, err.Error())
		return defaultValue
	}
	return result
}
