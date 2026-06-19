package httpproxy

import (
	"fmt"
	"net/http"

	"github.com/elazarl/goproxy"
)

// StartHTTPProxy starts an HTTP/HTTPS forward proxy on the given port.
// Plain HTTP requests are forwarded directly, while HTTPS is tunneled
// transparently via the CONNECT method (no MITM, traffic is not decrypted).
func StartHTTPProxy(port string) {
	proxy := goproxy.NewProxyHttpServer()
	addr := fmt.Sprintf("0.0.0.0:%s", port)

	for {
		if err := http.ListenAndServe(addr, proxy); err != nil {
			fmt.Println("http proxy server exit, restart")
		} else {
			fmt.Println("http proxy server started")
		}
	}
}
