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
	idleConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "idle_connections",
		Help: "Number of idle connections",
	})
	logEntries = promauto.NewCounter(prometheus.CounterOpts{
		Name: "log_entries_total",
		Help: "Total number of log entries",
	})
)

func main() {
	// Set up SOCKS5 server
	socksServer := utils.SetupSocks5Server()

	// Create a connection pool
	pool := proxy.NewConnectionPool(10)
	go pool.AutoScale()

	// Get the IP address of the eth0 interface
	eth0IP := utils.GetEth0IP()

	// Start a goroutine to listen for incoming connections
	go utils.ListenForConnections(socksServer, eth0IP)

	// Set up HTTP server
	httpServer := utils.SetupHTTPServer()

	// Handle termination signals
	handleTerminationSignals(httpServer, pool)

	// Start HTTP server
	utils.StartHTTPServer(httpServer)

	// Start a goroutine to print stats every 5 seconds
	go stats.PrintStats(pool)

	// Wait for termination signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	// Stop accepting new connections
	httpServer.Shutdown(context.Background())

	// Close all existing connections
	pool.Close()

	os.Exit(0)
}

func handleTerminationSignals(httpServer *http.Server, pool *proxy.ConnectionPool) {
	// Create a channel to listen for termination signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-quit

		// Stop accepting new connections
		if err := httpServer.Shutdown(context.Background()); err != nil {
			logrus.Fatal(err)
		}

		// Close all existing connections
		pool.Close()

		os.Exit(0)
	}()
}
