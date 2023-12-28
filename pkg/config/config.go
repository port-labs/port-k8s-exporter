package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/joho/godotenv"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"k8s.io/klog/v2"
	"strings"
)

var KafkaConfig = &KafkaConfiguration{}
var PollingListenerRate uint

var ApplicationConfig = &ApplicationConfiguration{}

func Init() {
	_ = godotenv.Load()

	NewString(&ApplicationConfig.EventListenerType, "event-listener-type", "POLLING", "Event listener type, can be either POLLING or KAFKA. Optional.")

	// Kafka listener Configuration
	NewString(&KafkaConfig.Brokers, "event-listener-brokers", "localhost:9092", "Kafka event listener brokers")
	NewString(&KafkaConfig.SecurityProtocol, "event-listener-security-protocol", "plaintext", "Kafka event listener security protocol")
	NewString(&KafkaConfig.AuthenticationMechanism, "event-listener-authentication-mechanism", "none", "Kafka event listener authentication mechanism")

	// Polling listener Configuration
	NewUInt(&PollingListenerRate, "event-listener-polling-rate", 60, "Polling event listener polling rate")

	// Application Configuration
	NewString(&ApplicationConfig.ConfigFilePath, "config", "config.yaml", "Path to Port K8s Exporter config file. Required.")
	NewString(&ApplicationConfig.StateKey, "state-key", "my-k8s-exporter", "Port K8s Exporter state key id. Required.")
	NewUInt(&ApplicationConfig.ResyncInterval, "resync-interval", 0, "The re-sync interval in minutes. Optional.")
	NewString(&ApplicationConfig.PortBaseURL, "port-base-url", "https://api.getport.io", "Port base URL. Optional.")
	NewString(&ApplicationConfig.PortClientId, "port-client-id", "", "Port client id. Required.")
	NewString(&ApplicationConfig.PortClientSecret, "port-client-secret", "", "Port client secret. Required.")
	NewBool(&ApplicationConfig.CreateDefaultResources, "create-default-resources", true, "Create default resources on installation. Optional.")

	// Deprecated
	NewBool(&ApplicationConfig.DeleteDependents, "delete-dependents", false, "Delete dependents. Optional.")
	NewBool(&ApplicationConfig.CreateMissingRelatedEntities, "create-missing-related-entities", false, "Create missing related entities. Optional.")

	flag.Parse()
}

func NewConfiguration() (*port.Config, error) {
	overrides := &port.Config{
		StateKey:                     ApplicationConfig.StateKey,
		EventListenerType:            ApplicationConfig.EventListenerType,
		CreateDefaultResources:       ApplicationConfig.CreateDefaultResources,
		ResyncInterval:               ApplicationConfig.ResyncInterval,
		CreateMissingRelatedEntities: ApplicationConfig.CreateMissingRelatedEntities,
		DeleteDependents:             ApplicationConfig.DeleteDependents,
	}

	c, err := GetConfigFile(ApplicationConfig.ConfigFilePath)
	var fileNotFoundError *FileNotFoundError
	if errors.As(err, &fileNotFoundError) {
		klog.Infof("Config file not found, using defaults")
		return overrides, nil
	}
	klog.Infof("Config file found")
	v, err := json.Marshal(overrides)
	if err != nil {
		return nil, fmt.Errorf("failed loading configuration: %w", err)
	}

	err = json.Unmarshal(v, &c)
	if err != nil {
		return nil, fmt.Errorf("failed loading configuration: %w", err)
	}

	c.StateKey = strings.ToLower(c.StateKey)

	return c, nil
}
