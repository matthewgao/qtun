package socks5

import "fmt"

func StartSocks5(port string) {
	conf := &Config{}
	server, err := New(conf)
	if err != nil {
		panic(err)
	}

	// Create SOCKS5 proxy on localhost port 8000
	addr := fmt.Sprintf("0.0.0.0:%s", port)

	for {
		if err := server.ListenAndServe("tcp", addr); err != nil {
			fmt.Println("socks5 server exit, restart")
		} else {
			fmt.Println("socks5 server started")
		}
	}
}
