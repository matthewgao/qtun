package config

type Config struct {
	Key              string `default:"hello-world"`
	RemoteAddrs      string `default:"0.0.0.0:8080"`
	Listen           string `default:"0.0.0.0:8080"`
	TransportThreads int    `default:"1"`
	Ip               string `default:"10.237.0.1/16"`
	Mtu              int    `default:"1400"`
	// Verbose          bool   `default:"0"`
	ServerMode bool `default:"0"`
	NoDelay    bool
	// FlowHash 开启「流亲和」：数据面 datagram 按五元组哈希固定分发到同一条连接。
	// 开启后多连接(transport_threads>1)聚合吞吐更高(多流并行、不跨连接乱序)，但单条流
	// 被钉在一条连接上、吞吐受单连接上限。默认关闭，恢复随机/轮询分发。
	FlowHash bool
	// UDP 开启后数据面/控制面都走「裸 UDP + AES-GCM」(类 WireGuard)，而非 QUIC。
	// 动机：QUIC 即便用 datagram，其拥塞控制/pacer/32 深发送队列会在「不丢包」的情况下
	// 把单条流限速(实测单流~20Mbps，而直连 iperf 远高于此)。裸 UDP 去掉这层，隧道变成
	// 透明的「哑管道」，把拥塞控制完全交还内层 TCP，单流可直接吃到真实路径带宽。
	// 默认开启；置为 false 可回退 QUIC 传输做 A/B 对比。
	UDP bool
	// EgressWorkers：读 TUN 并发送到对端的 worker 数。0 = 自动(2×CPU)。设为 1 可让单条流
	// 的包严格按读到顺序发出、消除「多 worker 抢着发同一条流」造成的乱序(轻微乱序不触发
	// 内层 TCP 的 3-dup-ACK、不显示为 retr，却会压住 cwnd)。用于排查单流吞吐天花板。
	EgressWorkers int
}

var GLOBAL_CONFIG *Config = nil

func InitConfig(cfg Config) {
	GLOBAL_CONFIG = &cfg
}

func GetInstance() *Config {
	return GLOBAL_CONFIG
}
