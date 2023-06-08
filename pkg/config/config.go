package config

import (
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"os"

	"gopkg.in/yaml.v2"
)

type Entity struct {
	Mappings []port.EntityMapping
}

type Port struct {
	Entity Entity
}

type Selector struct {
	Query string
}

type Resource struct {
	Kind     string
	Selector Selector
	Port     Port
}

type Config struct {
	Resources      []Resource
	ResyncInterval uint
	StateKey       string
}

type KindConfig struct {
	Selector Selector
	Port     Port
}

type AggregatedResource struct {
	Kind        string
	KindConfigs []KindConfig
}

func New(filepath string, resyncInterval uint, stateKey string) (*Config, error) {
	c := &Config{
		ResyncInterval: resyncInterval,
		StateKey:       stateKey,
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
