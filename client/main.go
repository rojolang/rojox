package client

import (
	"context"
	"fmt"
	"go.uber.org/zap"
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
	// Initialize zap logger
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "can't initialize zap logger: %v", err)
		return
	}
	defer func(logger *zap.Logger) {
		err := logger.Sync()
		if err != nil {

		}
	}(logger) // Flushes buffer, if any

	var wg sync.WaitGroup
	for i := 0; i < MaxGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			// Create a SOCKS5 dialer
			dialer, err := proxy.SOCKS5("tcp", SOCKS5ProxyAddr, nil, proxy.Direct)
			if err != nil {
				logger.Error("Can't connect to the proxy", zap.Error(err))
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
			sendHTTPRequest(logger, client, "https://www.google.com") // HTTPS request
		}(i)
	}
	wg.Wait()
}

func sendHTTPRequest(logger *zap.Logger, client *http.Client, url string) {
	// Create HTTP Request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		logger.Error("Can't create request", zap.String("url", url), zap.Error(err))
		return
	}

	// Send the request via the client
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("Can't do request", zap.String("url", url), zap.Error(err))
		return
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {

		}
	}(resp.Body)

	// Print the response
	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		logger.Error("Error reading body", zap.Error(err))
	}
}
