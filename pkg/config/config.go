package config

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/port-labs/port-k8s-exporter/pkg/port"
	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"
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
	NewCreatePortResourcesOrigin(&ApplicationConfig.CreatePortResourcesOrigin, "create-default-resources-origin", "Port", "Create default resources origin on installation. Optional.")

	NewBool(&ApplicationConfig.OverwriteConfigurationOnRestart, "overwrite-configuration-on-restart", false, "Overwrite the configuration in port on restarting the exporter. Optional.")

	// Deprecated
	NewBool(&ApplicationConfig.DeleteDependents, "delete-dependents", false, "Delete dependents. Optional.")
	NewBool(&ApplicationConfig.CreateMissingRelatedEntities, "create-missing-related-entities", false, "Create missing related entities. Optional.")

	flag.Parse()
}

func NewConfiguration() (*port.Config, error) {
	config := &port.Config{
		StateKey:                        ApplicationConfig.StateKey,
		EventListenerType:               ApplicationConfig.EventListenerType,
		CreateDefaultResources:          ApplicationConfig.CreateDefaultResources,
		CreatePortResourcesOrigin:       ApplicationConfig.CreatePortResourcesOrigin,
		ResyncInterval:                  ApplicationConfig.ResyncInterval,
		OverwriteConfigurationOnRestart: ApplicationConfig.OverwriteConfigurationOnRestart,
		CreateMissingRelatedEntities:    ApplicationConfig.CreateMissingRelatedEntities,
		DeleteDependents:                ApplicationConfig.DeleteDependents,
	}

	v, err := os.ReadFile(ApplicationConfig.ConfigFilePath)
	if err != nil {
		v = []byte("{}")
		klog.Infof("Config file not found, using defaults")
		return config, nil
	}
	klog.Infof("Config file found")
	err = yaml.Unmarshal(v, &config)
	if err != nil {
		return nil, fmt.Errorf("failed loading configuration: %w", err)
	}

	config.StateKey = strings.ToLower(config.StateKey)

	return config, nil
}
