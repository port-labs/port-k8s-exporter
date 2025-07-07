package config

import (
	"flag"
	"fmt"
	"strings"

	"github.com/port-labs/port-k8s-exporter/pkg/port"

	"github.com/port-labs/port-k8s-exporter/pkg/goutils"
	"k8s.io/utils/strings/slices"
)

var keys []string

func prepareEnvKey(key string) string {
	newKey := strings.ToUpper(strings.ReplaceAll(key, "-", "_"))

	if slices.Contains(keys, newKey) {
		panic("Application Error : Found duplicate config key: " + newKey)
	}

	keys = append(keys, newKey)
	return newKey
}

func NewString(v *string, key string, defaultValue string, description string) {
	value := goutils.GetStringEnvOrDefault(prepareEnvKey(key), defaultValue)
	flag.StringVar(v, key, value, description)
}

func NewUInt(v *uint, key string, defaultValue uint, description string) {
	value := uint(goutils.GetUintEnvOrDefault(prepareEnvKey(key), uint64(defaultValue)))
	flag.UintVar(v, key, value, description)
}

func NewInt(v *int, key string, defaultValue int, description string) {
	value := int(goutils.GetIntEnvOrDefault(prepareEnvKey(key), int64(defaultValue)))
	flag.IntVar(v, key, value, description)
}

func NewBool(v *bool, key string, defaultValue bool, description string) {
	value := goutils.GetBoolEnvOrDefault(prepareEnvKey(key), defaultValue)
	flag.BoolVar(v, key, value, description)
}

func NewCreatePortResourcesOrigin(target *port.CreatePortResourcesOrigin, key, defaultValue, description string) {
	var value string
	flag.StringVar(&value, key, defaultValue, description)

	*target = port.CreatePortResourcesOrigin(value)
	if *target != port.CreatePortResourcesOriginPort && *target != port.CreatePortResourcesOriginK8S {
		panic(fmt.Sprintf("Invalid value for %s: %s. Must be one of [Port, K8S]", key, value))
	}
}
