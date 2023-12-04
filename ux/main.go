package ux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rojolang/rojox/server"
	"go.uber.org/zap"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type ErrorWithContext struct {
	Context string
	Err     error
}

func (e *ErrorWithContext) Error() string {
	return fmt.Sprintf("%s: %v", e.Context, e.Err)
}

// Run starts the UX server with the given LoadBalancer.
func Run(lb *server.LoadBalancer) {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Set up the registration handler.
	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		registerHandler(logger, w, r, lb) // Pass the LoadBalancer instance to the handler
		// Increment Prometheus counter for registration requests here if needed
	})

	// Start the HTTP server on a separate port for registration and metrics.
	httpServer := startHTTPServer(":8080", logger)

	// Start listening for SOCKS connections on port 9050.
	go startListener(":9050", lb, logger)

	// Wait for termination signals and pass the httpServer to handleTerminationSignals.
	handleTerminationSignals(httpServer, logger)
}

func startHTTPServer(listenAddress string, logger *zap.Logger) *http.Server {
	logger.Info("Starting HTTP server on " + listenAddress)
	httpServer := &http.Server{
		Addr:    listenAddress,
		Handler: nil, // Default ServeMux.
	}

	// Set up the Prometheus metrics endpoint.
	http.Handle("/metrics", promhttp.Handler())

	// Listen for incoming connections.
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		logger.Fatal("Failed to set up listener", zap.Error(err))
	}

	// Handle incoming connections in a goroutine.
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("HTTP server failed", zap.Error(err))
		}
	}()

	return httpServer
}

func startListener(listenAddress string, lb *server.LoadBalancer, logger *zap.Logger) {
	logger.Info("Listening for incoming SOCKS connections on " + listenAddress)
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		logger.Fatal("Failed to set up SOCKS listener", zap.Error(err))
		return
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			logger.Error("Accepting SOCKS connection", zap.Error(err))
			break
		}
		go lb.HandleConnection(conn)
	}
}

func registerHandler(logger *zap.Logger, w http.ResponseWriter, r *http.Request, lb *server.LoadBalancer) {
	logger.Info("Received registration request", zap.String("remoteAddr", r.RemoteAddr))

	if r.Method != http.MethodPost {
		msg := "Invalid method"
		http.Error(w, msg, http.StatusMethodNotAllowed)
		logger.Error(msg, zap.String("method", r.Method))
		return
	}

	ip, err := parseRequest(logger, r)
	if err != nil {
		msg := "Bad request"
		http.Error(w, msg, http.StatusBadRequest)
		logger.Error(msg, zap.Error(err))
		return
	}

	logger.Info("Registering satellite", zap.String("ip", ip))
	lb.RegisterSatellite(ip) // Register satellite with the LoadBalancer
	if _, err := fmt.Fprintln(w, "Registered new satellite:", ip); err != nil {
		logger.Error("writing response", zap.Error(err))
	}
}

func parseRequest(logger *zap.Logger, r *http.Request) (string, error) {
	var data map[string]string
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		logger.Error("decoding request body", zap.Error(err))
		return "", err
	}

	ip, ok := data["ip"]
	if !ok {
		err := fmt.Errorf("IP not provided in request")
		logger.Error("getting IP from request", zap.Error(err))
		return "", err
	}

	logger.Info("Parsed IP from request", zap.String("ip", ip))
	return ip, nil
}
func handleTerminationSignals(httpServer *http.Server, logger *zap.Logger) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	<-quit
	logger.Info("Received termination signal")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("Error shutting down HTTP server", zap.Error(err))
	}
}
