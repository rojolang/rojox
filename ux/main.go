package ux

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/sirupsen/logrus"
)

// A map to store the IP addresses of registered satellites
var (
	satellites = make(map[string]bool)
	mu         sync.Mutex
)

func Run() {
	// Set up logging
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.InfoLevel)

	// Register the /register endpoint
	http.HandleFunc("/register", registerHandler)

	// Start the HTTP server
	logrus.Info("Starting HTTP server")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		logrus.WithFields(logrus.Fields{"context": "starting HTTP server"}).Fatal(err)
	}
}

// registerHandler handles registration requests from satellite servers.
// It expects a POST request with a JSON body containing the IP address of the satellite server.
func registerHandler(w http.ResponseWriter, r *http.Request) {
	logrus.Info("Received registration request")

	// Check for POST method
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return
	}

	// Parse the request body
	var data map[string]string
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Get the IP address from the request
	ip, ok := data["ip"]
	if !ok {
		http.Error(w, "IP not provided", http.StatusBadRequest)
		return
	}

	// Register the satellite
	logrus.WithField("ip", ip).Info("Registering satellite")
	mu.Lock()
	satellites[ip] = true
	mu.Unlock()

	// Update the Prometheus configuration
	if err := updatePrometheusConfiguration(ip); err != nil {
		logrus.WithFields(logrus.Fields{"context": "updating Prometheus configuration"}).Error(err)
		return
	}

	// Respond with a success message
	fmt.Fprintln(w, "Registered new satellite:", ip)
}

// updatePrometheusConfiguration updates the Prometheus configuration to scrape metrics from the new satellite.
func updatePrometheusConfiguration(ip string) error {
	logrus.WithField("ip", ip).Info("Updating Prometheus configuration")

	// Read the existing configuration
	config, err := os.ReadFile("./docker/prometheus.yml")
	if err != nil {
		return fmt.Errorf("failed to read Prometheus configuration: %w", err)
	}

	// Append the new satellite's IP address
	config = append(config, []byte("\n  - "+ip+":8080")...)

	// Write the updated configuration back to the file
	if err := os.WriteFile("./docker/prometheus.yml", config, 0644); err != nil {
		return fmt.Errorf("failed to write Prometheus configuration: %w", err)
	}

	// Reload the Prometheus configuration
	resp, err := http.Post("http://localhost:9090/-/reload", "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to reload Prometheus configuration: %w", err)
	}
	defer resp.Body.Close()

	// Check the response status
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}
		return fmt.Errorf("failed to reload Prometheus configuration: %s", body)
	}

	return nil
}
