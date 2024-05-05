package config

import "github.com/port-labs/port-k8s-exporter/pkg/port"

type KafkaConfiguration struct {
	Brokers                 string
	SecurityProtocol        string
	GroupID                 string
	AuthenticationMechanism string
	Username                string
	Password                string
	KafkaSecurityEnabled    bool
}

type ApplicationConfiguration struct {
	ConfigFilePath                  string
	StateKey                        string
	ResyncInterval                  uint
	PortBaseURL                     string
	PortClientId                    string
	PortClientSecret                string
	EventListenerType               string
	CreateDefaultResources          bool
	OverwriteConfigurationOnRestart bool
	// These Configurations are used only for setting up the Integration on installation or when using OverwriteConfigurationOnRestart flag.
	Resources                    []port.Resource
	DeleteDependents             bool `json:"deleteDependents,omitempty"`
	CreateMissingRelatedEntities bool `json:"createMissingRelatedEntities,omitempty"`
}
