package config

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
	ConfigFilePath    string
	StateKey          string
	ResyncInterval    uint
	PortBaseURL       string
	PortClientId      string
	PortClientSecret  string
	EventListenerType string
}
