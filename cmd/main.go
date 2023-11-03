package main

import (
	"context"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rojolang/rojox/proxy"
	"github.com/rojolang/rojox/stats"
	"github.com/rojolang/rojox/utils"
	"github.com/sirupsen/logrus"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

var (
	// Define metrics for active and idle connections
	activeConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "active_connections",
		Help: "Number of active connections",
	})
	logEntries = promauto.NewCounter(prometheus.CounterOpts{
		Name: "log_entries_total",
		Help: "Total number of log entries",
	})
)

func main() {
	logrus.SetLevel(logrus.InfoLevel)
	logrus.SetOutput(os.Stdout)
	logrus.Info("Starting main function")

	// Set up SOCKS5 server
	socksServer, err := utils.SetupSocks5Server()
	if err != nil {
		logrus.Fatalf("Failed to set up SOCKS5 server: %v", err)
	}

	// Create a connection manager
	manager := proxy.NewConnectionManager()

	// Get the IP address of the eth0 interface
	eth0IP, err := utils.GetEth0IP()
	if err != nil {
		logrus.Fatalf("Failed to get IP address of eth0: %v", err)
	}

	// Start a goroutine to listen for incoming connections
	go utils.ListenForConnections(socksServer, eth0IP, manager)

	// Set up HTTP server
	httpServer, err := utils.SetupHTTPServer()
	if err != nil {
		logrus.Fatalf("Failed to set up HTTP server: %v", err)
	}

	// Handle termination signals
	logrus.Info("Handling termination signals")
	handleTerminationSignals(httpServer)
	logrus.Info("Termination signals handled")

	// Start HTTP server in a separate goroutine
	logrus.Info("Starting HTTP server")
	go func() {
		if err := utils.StartHTTPServer(httpServer); err != nil {
			logrus.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()
	logrus.Info("HTTP server started")

	// Start a goroutine to print stats every 5 seconds
	logrus.Info("Starting PrintStats goroutine")
	go stats.PrintStats(manager)
	logrus.Info("PrintStats goroutine started")

	// Wait for termination signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	// Stop accepting new connections
	if err := httpServer.Shutdown(context.Background()); err != nil {
		logrus.Errorf("Failed to shutdown HTTP server: %v", err)
	}
}

func handleTerminationSignals(httpServer *http.Server) {
	// Create a channel to listen for termination signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-quit

		// Stop accepting new connections
		if err := httpServer.Shutdown(context.Background()); err != nil {
			logrus.Errorf("Failed to shutdown HTTP server: %v", err)
		}
	}()
}
