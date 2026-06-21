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
	NoDelay    bool
	// FlowHash 开启「流亲和」：数据面 datagram 按五元组哈希固定分发到同一条连接。
	// 开启后多连接(transport_threads>1)聚合吞吐更高(多流并行、不跨连接乱序)，但单条流
	// 被钉在一条连接上、吞吐受单连接上限。默认关闭，恢复随机/轮询分发。
	FlowHash bool
}

var GLOBAL_CONFIG *Config = nil

func InitConfig(cfg Config) {
	GLOBAL_CONFIG = &cfg
}

func GetInstance() *Config {
	return GLOBAL_CONFIG
}
