package config

import (
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"os"

	"gopkg.in/yaml.v2"
)

type FileNotFoundError struct {
	s string
}

func (e *FileNotFoundError) Error() string {
	return e.s
}

func New(filepath string, resyncInterval uint, stateKey string, eventListenerType string) (*port.Config, error) {
	c := &port.Config{
		ResyncInterval:    resyncInterval,
		StateKey:          stateKey,
		EventListenerType: eventListenerType,
	}
	config, err := os.ReadFile(filepath)
	if err != nil {
		return c, &FileNotFoundError{err.Error()}
	}

	err = yaml.Unmarshal(config, c)
	if err != nil {
		return nil, err
	}

	return c, nil
}
