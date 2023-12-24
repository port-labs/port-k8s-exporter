package config

import (
	"flag"
	"github.com/joho/godotenv"
)

var KafkaConfig = &KafkaConfiguration{}
var PollingListenerRate uint

var ApplicationConfig = &ApplicationConfiguration{}

func Init() {
	_ = godotenv.Load()
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
	NewString(&ApplicationConfig.EventListenerType, "event-listener-type", "POLLING", "Event listener type, can be either POLLING or KAFKA. Optional.")

	flag.Parse()
}
