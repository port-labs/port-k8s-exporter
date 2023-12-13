package config

var KafkaConfig = &KafkaConfiguration{
	Brokers:                 NewString("event-listener-brokers", "localhost:9092", "Kafka brokers"),
	SecurityProtocol:        NewString("event-listener-security-protocol", "plaintext", "Kafka security protocol"),
	AuthenticationMechanism: NewString("event-listener-authentication-mechanism", "none", "Kafka authentication mechanism"),
}
var PollingListenerRate = NewUInt("event-listener-polling-rate", 60, "Polling rate for the polling event listener")

var ApplicationConfig = &ApplicationConfiguration{
	ConfigFilePath:    NewString("config", "", "Path to Port K8s Exporter config file. Required."),
	StateKey:          NewString("state-key", "", "Port K8s Exporter state key id. Required."),
	ResyncInterval:    NewUInt("resync-interval", 0, "The re-sync interval in minutes. Optional."),
	PortBaseURL:       NewString("port-base-url", "https://api.getport.io", "Port base URL. Optional."),
	PortClientId:      NewString("port-client-id", "", "Port client id. Required."),
	PortClientSecret:  NewString("port-client-secret", "", "Port client secret. Required."),
	EventListenerType: NewString("event-listener-type", "POLLING", "Event listener type. Optional."),
}
