package client

import (
	"context"
	"fmt"
	"golang.org/x/net/proxy"
	"golang.org/x/net/websocket"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
)

const (
	MaxGoroutines = 10 // Maximum number of goroutines to use for the stress test
)

func Run() {
	var wg sync.WaitGroup
	for i := 0; i < MaxGoroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			// Create a SOCKS5 dialer
			dialer, err := proxy.SOCKS5("tcp", "35.87.31.126:1080", nil, proxy.Direct)
			if err != nil {
				fmt.Fprintln(os.Stderr, "can't connect to the proxy:", err)
				return
			}

			// Setup HTTP transport
			httpTransport := &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (c net.Conn, err error) {
					return dialer.Dial(network, addr)
				},
			}

			// Create and configure a vanilla http.Client
			client := &http.Client{
				Transport: httpTransport,
			}

			if i < MaxGoroutines/4 {
				sendHTTPRequest(client, "http://google.com") // HTTP request
			} else if i < MaxGoroutines/2 {
				sendHTTPRequest(client, "https://google.com") // HTTPS request
			} else if i < 3*MaxGoroutines/4 {
				sendWebSocketRequest() // WebSocket request
			} else {
				sendSMTPRequest(dialer) // SMTP request
			}
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

func sendWebSocketRequest() {
	// Dial WebSocket server
	conn, err := websocket.Dial("ws://websocket.example.com", "", "http://localhost/")
	if err != nil {
		fmt.Fprintln(os.Stderr, "can't dial WebSocket server:", err)
		return
	}
	defer conn.Close()

	// Send a basic message
	err = websocket.Message.Send(conn, "Hello, WebSocket!")
	if err != nil {
		fmt.Fprintln(os.Stderr, "can't send message:", err)
		return
	}

	// Read the response
	var msg string
	err = websocket.Message.Receive(conn, &msg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "can't read response:", err)
		return
	}

	// Print the response
	fmt.Println(msg)
}

func sendSMTPRequest(dialer proxy.Dialer) {
	// Dial SMTP server
	conn, err := dialer.Dial("tcp", "smtp.example.com:25")
	if err != nil {
		fmt.Fprintln(os.Stderr, "can't dial SMTP server:", err)
		return
	}
	defer conn.Close()

	// Send a basic message
	_, err = conn.Write([]byte("HELO localhost\r\n"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "can't send message:", err)
		return
	}

	// Read the response
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		fmt.Fprintln(os.Stderr, "can't read response:", err)
		return
	}

	// Print the response
	fmt.Println(string(buf[:n]))
}
