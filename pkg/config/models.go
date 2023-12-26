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
	ConfigFilePath         string
	StateKey               string
	ResyncInterval         uint
	PortBaseURL            string
	PortClientId           string
	PortClientSecret       string
	EventListenerType      string
	CreateDefaultResources bool
	// Deprecated: use IntegrationAppConfig instead. Used for updating the Port integration config on startup.
	Resources []port.Resource
}
