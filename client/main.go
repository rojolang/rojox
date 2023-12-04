package client

import (
	"context"
	"fmt"
	"golang.org/x/net/proxy"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
)

const (
	MaxGoroutines   = 10                   // Maximum number of goroutines to use for the stress test
	SOCKS5ProxyAddr = "10.243.32.249:9050" // Replace with your satellite's ZeroTier IP and the proxy port
)

func Run() {
	var wg sync.WaitGroup
	for i := 0; i < MaxGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			// Create a SOCKS5 dialer
			dialer, err := proxy.SOCKS5("tcp", SOCKS5ProxyAddr, nil, proxy.Direct)
			if err != nil {
				fmt.Fprintln(os.Stderr, "can't connect to the proxy:", err)
				return
			}

			// Setup HTTP transport
			httpTransport := &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialer.Dial(network, addr)
				},
			}

			// Create and configure a vanilla http.Client
			client := &http.Client{
				Transport: httpTransport,
			}

			// Send HTTP request
			sendHTTPRequest(client, "https://www.google.com") // HTTPS request
		}(i)
	}
	wg.Wait()
}

func sendHTTPRequest(client *http.Client, url string) {
	// Create HTTP Request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "can't create request:", err)
		return
	}

	// Send the request via the client
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "can't do request:", err)
		return
	}
	defer resp.Body.Close()

	// Print the response
	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		fmt.Fprintln(os.Stderr, "error reading body:", err)
		return
	}
}
