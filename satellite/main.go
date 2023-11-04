package satellite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rojolang/rojox/proxy"
	"github.com/rojolang/rojox/stats"
	"github.com/rojolang/rojox/utils"
	"github.com/sirupsen/logrus"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// A map to store the IP addresses of registered satellites
var (
	satellites = make(map[string]bool)
	mu         sync.Mutex
)

func Run() {
	logrus.SetLevel(logrus.InfoLevel)
	logrus.SetOutput(os.Stdout)
	logrus.Info("Starting main function")

	// Set up SOCKS5 server
	socksServer, err := utils.SetupSocks5Server()
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "setting up SOCKS5 server"}).Fatal(err)
	}

	// Create a connection manager
	manager := proxy.NewConnectionManager()

	// Get the IP address of the eth0 interface
	eth0IP, err := utils.GetEth0IP()
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "getting IP of eth0"}).Fatal(err)
	}

	// Start a goroutine to listen for incoming connections
	listener, err := utils.ListenForConnections(socksServer, eth0IP, manager)
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "listening for connections"}).Fatal(err)
	}

	// Set up HTTP server
	httpServer, err := utils.SetupHTTPServer()
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "setting up HTTP server"}).Fatal(err)
	}

	// Expose metrics endpoint for Prometheus
	http.Handle("/metrics-satellite", promhttp.Handler())

	// Handle termination signals
	logrus.Info("Handling termination signals")
	handleTerminationSignals(httpServer, listener)
	logrus.Info("Termination signals handled")

	// Start HTTP server in a separate goroutine
	logrus.Info("Starting HTTP server")
	go func() {
		if err := utils.StartHTTPServer(httpServer); err != nil {
			logrus.WithFields(logrus.Fields{"context": "starting HTTP server"}).Fatal(err)
		}
	}()
	logrus.Info("HTTP server started")

	// Start a goroutine to print stats every 5 seconds
	logrus.Info("Starting PrintStats goroutine")
	go stats.PrintStats(manager)
	logrus.Info("PrintStats goroutine started")

	// Register with the UX server
	if err := registerWithUXServer(eth0IP.String()); err != nil {
		logrus.WithFields(logrus.Fields{"context": "registering with UX server"}).Fatal(err)
	}

	// Wait for termination signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	// Stop accepting new connections
	if err := httpServer.Shutdown(context.Background()); err != nil {
		logrus.WithFields(logrus.Fields{"context": "shutting down HTTP server"}).Error(err)
	}
}

// handleTerminationSignals sets up a goroutine to listen for termination signals and
// stops accepting new connections and closes the SOCKS5 server listener when a termination
// signal is received.
func handleTerminationSignals(httpServer *http.Server, listener net.Listener) {
	// Create a channel to listen for termination signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-quit

		// Stop accepting new connections
		if err := httpServer.Shutdown(context.Background()); err != nil {
			logrus.WithFields(logrus.Fields{"context": "shutting down HTTP server"}).Error(err)
		}

		// Close the SOCKS5 server listener
		if err := listener.Close(); err != nil {
			logrus.WithFields(logrus.Fields{"context": "closing SOCKS5 server listener"}).Error(err)
		}
	}()
}

// registerWithUXServer sends a registration request to the UX server with the IP address
// of the satellite server. It returns an error if the registration fails.
func registerWithUXServer(ip string) error {
	// Create the registration request
	reqBody, err := json.Marshal(map[string]string{"ip": ip})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", "http://35.87.31.126/register", bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	// Send the registration request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check the response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registration failed: status code %d", resp.StatusCode)
	}

	return nil
}
