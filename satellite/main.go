package satellite

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/rojolang/rojox/utils"
	"go.uber.org/zap"
)

const UXServerIP = "http://34.209.231.131:8080/register"

var (
	readTimeout  = 5 * time.Second
	writeTimeout = 10 * time.Second
)

type SimpleDialer struct{}

func (d *SimpleDialer) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}

	var dialAddr string
	for _, ip := range ips {
		if ip.To4() == nil && ip.IsGlobalUnicast() {
			dialAddr = net.JoinHostPort(ip.String(), port)
			network = "tcp6"
			break
		}
	}

	if dialAddr == "" {
		dialAddr = address
		network = "tcp4"
	}

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, network, dialAddr)
	if err != nil {
		return nil, err
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

	socksServer, err := utils.SetupSocks5Server()
	if err != nil {
		logger.Fatal("Failed to set up SOCKS5 server", zap.Error(err))
	}

	zeroTierIPString, err := getZeroTierIP(logger)
	if err != nil {
		logger.Fatal("Failed to get ZeroTier IP", zap.Error(err))
	}

	// Use the ZeroTier IP for registration
	if err := registerWithUXServer(UXServerIP, zeroTierIPString, logger); err != nil {
		logger.Fatal("Failed to register with UX server", zap.Error(err))
	}

	listener, err := utils.ListenForConnections(socksServer, net.ParseIP(zeroTierIPString), manager)
	if err != nil {
		logger.Fatal("Failed to listen for connections", zap.Error(err))
	}

	httpServer := &http.Server{
		Addr:         ":8080",
		Handler:      bufioHandler(promhttp.Handler(), logger),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	go func() {
		if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("HTTP server failed", zap.Error(err))
		}
	}()

	go stats.PrintStats(manager)
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
		bwResponseWriter := NewBufioResponseWriter(w, logger)
		defer bwResponseWriter.Flush()

		h.ServeHTTP(bwResponseWriter, r)
	})
}

func handleTerminationSignals(httpServer *http.Server, listener net.Listener, logger *zap.Logger) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	<-quit
	logger.Info("Received termination signal")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("Error shutting down HTTP server", zap.Error(err))
	}

	if err := listener.Close(); err != nil {
		logger.Error("Error closing SOCKS5 server listener", zap.Error(err))
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

		logger.Info("Sending registration request", zap.Any("request", req))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logger.Error("Sending register request failed", zap.Error(err))
			time.Sleep(1 * time.Second)
			continue
		}
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				logger.Error("Closing response body failed", zap.Error(err))
			}
		}(resp.Body)

		logger.Info("Received registration response", zap.Any("response", resp))

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
