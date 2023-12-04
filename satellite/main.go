package satellite

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rojolang/rojox/utils"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rojolang/rojox/proxy"
	"github.com/rojolang/rojox/stats"

	"go.uber.org/zap"
)

const UXServerIP = "http://34.209.231.131:8080/register"

var (
	readTimeout  = 10 * time.Second
	writeTimeout = 10 * time.Second
)

type SimpleDialer struct{}

func (d *SimpleDialer) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("failed to split network address: %w", err)
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("failed to look up IP for host: %w", err)
	}

	var dialAddr string
	var conn net.Conn
	dialer := &net.Dialer{}

	// Iterate over the IPs and select the first IPv6 address found.
	for _, ip := range ips {
		if ip.To4() == nil && ip.IsGlobalUnicast() {
			// Use the IPv6 address if available.
			dialAddr = net.JoinHostPort(ip.String(), port)
			conn, err = dialer.DialContext(ctx, "tcp6", dialAddr)
			if err == nil {
				return conn, nil
			}
			// Log the error and try the next IP if IPv6 connection fails.
			zap.L().Error("failed to dial IPv6", zap.Error(err), zap.String("address", dialAddr))
		}
	}

	// If no IPv6 addresses are available or all attempts fail, fall back to IPv4.
	if dialAddr == "" {
		dialAddr = net.JoinHostPort(host, port)
		conn, err = dialer.DialContext(ctx, "tcp4", dialAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to dial IPv4: %w", err)
		}
	}

	return conn, nil
}
func getZeroTierIP(logger *zap.Logger) (string, error) {
	cmd := exec.Command("sudo", "zerotier-cli", "listnetworks")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		logger.Error("Failed to execute zerotier-cli command", zap.Error(err))
		return "", err
	}

	output := stdout.String()
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 8 && fields[2] == "fada62b0151e0f56" {
			ipWithMask := fields[8]
			ip := strings.Split(ipWithMask, "/")[0]
			return ip, nil
		}
	}

	logger.Error("ZeroTier IP not found")
	return "", errors.New("ZeroTier IP not found")
}

func Run() {
	logger, _ := zap.NewProduction()
	defer func(logger *zap.Logger) {
		err := logger.Sync()
		if err != nil {
			logger.Error("Failed to sync logger", zap.Error(err))
		}
	}(logger)

	dialer := &SimpleDialer{}
	manager := proxy.NewConnectionManager(dialer)

	socksServer, err := proxy.SetupSocks5Server()
	if err != nil {
		logger.Fatal("Failed to set up SOCKS5 server", zap.Error(err))
	}

	zeroTierIPString, err := getZeroTierIP(logger)
	if err != nil {
		logger.Fatal("Failed to get ZeroTier IP", zap.Error(err))
	}

	if err := registerWithUXServer(UXServerIP, zeroTierIPString, logger); err != nil {
		logger.Fatal("Failed to register with UX server", zap.Error(err))
	}

	listener, err := utils.ListenForConnections(socksServer, net.ParseIP(zeroTierIPString), manager)
	if err != nil {
		logger.Fatal("Failed to listen for connections", zap.Error(err))
	}

	httpServer := &http.Server{
		Addr:         ":8080",
		Handler:      nil, // Use Default ServeMux which we will attach handlers to.
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	// Set up the Prometheus metrics endpoint.
	http.Handle("/metrics", promhttp.Handler())

	// Start the HTTP server in a goroutine so it doesn't block.
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("HTTP server failed", zap.Error(err))
		}
	}()

	// Start the stats printing routine using the ConnectionManager.
	go stats.PrintStats(manager)

	// Handle termination signals for graceful shutdown.
	handleTerminationSignals(httpServer, listener, logger)
}

// BufioResponseWriter embeds http.ResponseWriter and a bufio.Writer to enable buffered writing.
type BufioResponseWriter struct {
	http.ResponseWriter
	writer *bufio.Writer
	logger *zap.Logger
}

// NewBufioResponseWriter creates a new BufioResponseWriter with the provided http.ResponseWriter and logger.
func NewBufioResponseWriter(w http.ResponseWriter, logger *zap.Logger) *BufioResponseWriter {
	return &BufioResponseWriter{
		ResponseWriter: w,
		writer:         bufio.NewWriter(w),
		logger:         logger,
	}
}

// Write uses the bufio.Writer to write.
func (b *BufioResponseWriter) Write(data []byte) (int, error) {
	return b.writer.Write(data)
}

// Flush ensures that any buffered data is flushed to the underlying ResponseWriter.
func (b *BufioResponseWriter) Flush() {
	if err := b.writer.Flush(); err != nil {
		b.logger.Error("Failed to flush buffered writer", zap.Error(err))
	}
}

// bufioHandler wraps an HTTP handler to provide buffered writing for responses.
func bufioHandler(h http.Handler, logger *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" { // Only buffer the Prometheus metrics endpoint
			bwResponseWriter := NewBufioResponseWriter(w, logger)
			defer bwResponseWriter.Flush() // Ensure we flush the buffer at the end of the request

			h.ServeHTTP(bwResponseWriter, r)
		} else {
			h.ServeHTTP(w, r) // Serve normally for all other endpoints
		}
	})
}

func handleTerminationSignals(httpServer *http.Server, listener net.Listener, logger *zap.Logger) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	<-quit
	logger.Info("Received termination signal")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := listener.Close(); err != nil {
		logger.Error("Error closing SOCKS5 server listener", zap.Error(err))
	}

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("Error shutting down HTTP server", zap.Error(err))
	}
}

func registerWithUXServer(uxServerIP, zeroTierIP string, logger *zap.Logger) error {
	logger.Debug("Satellite registerWithUXServer function started")
	for i := 0; i < 3; i++ {
		logger.Info("Registering IP", zap.String("ip", zeroTierIP))

		reqBody, err := json.Marshal(map[string]string{"ip": zeroTierIP})
		if err != nil {
			logger.Error("Creating register request failed", zap.Error(err))
			time.Sleep(1 * time.Second)
			continue
		}
		req, err := http.NewRequest("POST", uxServerIP, bytes.NewBuffer(reqBody))
		if err != nil {
			logger.Error("Creating new request failed", zap.Error(err))
			time.Sleep(1 * time.Second)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		logger.Info("Sending registration request")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logger.Error("Sending register request failed", zap.Error(err))
			time.Sleep(1 * time.Second)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			err = fmt.Errorf("registration failed: status code %d", resp.StatusCode)
			logger.Error("Register response failed", zap.Error(err))
			time.Sleep(1 * time.Second)
			continue
		}

		logger.Info("Successfully registered with UX server")
		return nil
	}
	logger.Debug("Satellite registerWithUXServer function finished")
	return fmt.Errorf("registration failed after 3 attempts")
}
