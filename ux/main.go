package ux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// Run starts the UX server with the given LoadBalancer.
func Run(lb *server.LoadBalancer) {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		registerHandler(logger, w, r, lb) // Pass the LoadBalancer instance to the handler
	})

	listenAddress := "0.0.0.0:8080" // Replace with the specific IP if necessary.
	logger.Info("Starting HTTP server on " + listenAddress)
	httpServer := &http.Server{ // Renamed variable to httpServer
		Addr:    listenAddress,
		Handler: nil, // Default ServeMux.
	}

	// Listen for incoming connections.
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		logger.Fatal("Failed to set up listener", zap.Error(err))
	}
	defer listener.Close()

	// Handle incoming connections.
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("HTTP server failed", zap.Error(err))
		}
	}()

	// Call the handleTerminationSignals function to handle graceful shutdown.
	handleTerminationSignals(httpServer, logger) // Corrected to pass httpServer
}

func startListener(logger *zap.Logger, lb *server.LoadBalancer) {
	for {
		logger.Info("Listening for incoming connections")
		listener, err := net.Listen("tcp", ":9050")
		if err != nil {
			logger.Error("listening for connections", zap.Error(err))
			return
		}
		defer func(listener net.Listener) {
			err := listener.Close()
			if err != nil {
				logger.Error("closing listener", zap.Error(err))
			}
		}(listener)

		for {
			conn, err := listener.Accept()
			if err != nil {
				logger.Error("accepting connection", zap.Error(err))
				break
			}
			go lb.HandleConnection(conn)
		}
		time.Sleep(1 * time.Second) // If the listener breaks, wait a second before retrying
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
