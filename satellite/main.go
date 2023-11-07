package satellite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rojolang/rojox/proxy"
	"github.com/rojolang/rojox/stats"
	"github.com/rojolang/rojox/utils"
	"github.com/sirupsen/logrus"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// UXServerIP is the IP address of the UX server.
const UXServerIP = "http://35.87.31.126:8080/register" // replace with the actual IP address of your UX server

// SimpleDialer is a Dialer that uses net.Dial to create connections.
type SimpleDialer struct{}

func (d *SimpleDialer) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	logrus.WithFields(logrus.Fields{"network": network, "address": address}).Info("Dialing...") // Added info print
	return net.Dial(network, address)
}

func getZeroTierIP() (string, error) {
	cmd := exec.Command("zerotier-cli", "listnetworks")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(output), "\n")
	logrus.Info("ZeroTier networks: ", lines)

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 7 && fields[1] == "fada62b0151e0f56" {
			ipWithMask := fields[7]
			ip := strings.Split(ipWithMask, "/")[0] // Split the string by '/' and take the first part
			logrus.Info("ZeroTier IP found: ", ip)
			return ip, nil
		}
	}

	return "", errors.New("ZeroTier IP not found")
}

func Run() {
	logrus.SetLevel(logrus.DebugLevel) // Set log level to Debug
	logrus.SetOutput(os.Stdout)
	logrus.Info("Starting main function")

	// Set up SOCKS5 server
	socksServer, err := utils.SetupSocks5Server()
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "setting up SOCKS5 server"}).Fatal(err)
	}

	// Create a connection manager
	dialer := &SimpleDialer{}
	manager := proxy.NewConnectionManager(dialer)

	// Get the IP address of the eth0 interface
	eth0IP, err := utils.GetEth0IP()
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "getting IP of eth0"}).Fatal(err)
	}

	// Register with the UX server
	logrus.Info("Registering with UX server")
	if err := registerWithUXServer(UXServerIP); err != nil {
		logrus.WithFields(logrus.Fields{"context": "registering with UX server"}).Fatal(err)
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

		logrus.Info("Received termination signal") // Added info print

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
func registerWithUXServer(uxServerIP string) error {
	for i := 0; i < 3; i++ { // Try to register 3 times
		ip, err := getZeroTierIP()
		if err != nil {
			logrus.WithField("context", "getting ZeroTier IP").Error(err)
			time.Sleep(1 * time.Second) // Wait for 1 second before retrying
			continue
		}
		logrus.Info("Registering IP: ", ip) // Added info print

		// Create the registration request
		reqBody, err := json.Marshal(map[string]string{"ip": ip})
		if err != nil {
			logrus.WithField("context", "creating register request").Error(err)
			time.Sleep(1 * time.Second) // Wait for 1 second before retrying
			continue
		}
		req, err := http.NewRequest("POST", uxServerIP, bytes.NewBuffer(reqBody))

		if err != nil {
			logrus.WithField("context", "creating new request").Error(err)
			time.Sleep(1 * time.Second) // Wait for 1 second before retrying
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		// Send the registration request
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logrus.WithField("context", "sending register request").Error(err)
			time.Sleep(1 * time.Second) // Wait for 1 second before retrying
			continue
		}
		defer resp.Body.Close()

		// Check the response
		if resp.StatusCode != http.StatusOK {
			err = fmt.Errorf("registration failed: status code %d", resp.StatusCode)
			logrus.WithField("context", "register response").Error(err)
			time.Sleep(1 * time.Second) // Wait for 1 second before retrying
			continue
		}

		logrus.Info("Successfully registered with UX server") // Added info print
		return nil                                            // If registration is successful, return nil
	}

	return fmt.Errorf("registration failed after 3 attempts") // If registration fails after 3 attempts, return an error
}
