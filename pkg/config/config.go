package config

import (
	"flag"
	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"gopkg.in/yaml.v2"
	"k8s.io/klog/v2"
	"os"
	"slices"
	"strings"
)

var keys []string

func prepareEnvKey(key string) string {
	newKey := strings.ToUpper(strings.ReplaceAll(key, "-", "_"))

	if slices.Contains(keys, newKey) {
		klog.Fatalf("Application Error : Found duplicate config key: %s", newKey)
	}

	keys = append(keys, newKey)
	return newKey
}

func NewString(key string, defaultValue string, description string) string {
	var value string
	flag.StringVar(&value, key, "", description)
	if value == "" {
		value = goutils.GetStringEnvOrDefault(prepareEnvKey(key), defaultValue)
	}

	return value
}

func NewUInt(key string, defaultValue uint, description string) uint {
	var value uint64
	flag.Uint64Var(&value, key, 0, description)
	if value == 0 {
		value = goutils.GetUintEnvOrDefault(prepareEnvKey(key), uint64(defaultValue))
	}

	return uint(value)
}

func GetConfigFile(filepath string, resyncInterval uint, stateKey string, eventListenerType string) (*port.Config, error) {
	c := &port.Config{
		ResyncInterval:    resyncInterval,
		StateKey:          stateKey,
		EventListenerType: eventListenerType,
	}
	config, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(config, c)
	if err != nil {
		return nil, err
	}

	return c, nil
}
