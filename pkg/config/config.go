package config

import (
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"os"

	"gopkg.in/yaml.v2"
)

func New(filepath string, resyncInterval uint, stateKey string, eventListenerType string) (*port.Config, error) {
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
