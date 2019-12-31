package config

type Config struct {
	Key              string `default:"hello-world"`
	RemoteAddrs      string `default:"0.0.0.0:8080"`
	Listen           string `default:"0.0.0.0:8080"`
	TransportThreads int    `default:"1"`
	Ip               string `default:"10.237.0.1/16"`
	Mtu              int    `default:"1500"`
	// Verbose          bool   `default:"0"`
	ServerMode bool `default:"0"`
}

var GLOBAL_CONFIG *Config = nil

func InitConfig(cfg Config) {
	GLOBAL_CONFIG = &cfg
}

func GetInstance() *Config {
	return GLOBAL_CONFIG
}
