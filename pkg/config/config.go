package config

import (
	"os"

	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Resources      []port.Resource
	ResyncInterval uint
	StateKey       string
}

func New(filepath string, resyncInterval uint, stateKey string, integration *port.Integration) (*Config, error) {

	c := &Config{
		ResyncInterval: resyncInterval,
		StateKey:       stateKey,
	}
	if integration.Config.Resources != nil {
		c.Resources = integration.Config.Resources
		return c, nil
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
