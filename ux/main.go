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

// ErrorWithContext is a custom error type that includes additional context about the error.
type ErrorWithContext struct {
	Context string
	Err     error
}

func (e *ErrorWithContext) Error() string {
	return fmt.Sprintf("%s: %v", e.Context, e.Err)
}

// satellites is a map to store the IP addresses of registered satellites.
var (
	satellites = make(map[string]bool)
	mu         sync.Mutex
)

// Run starts the HTTP server and registers the /register endpoint.
func Run() {
	// Set up logging
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.InfoLevel)

	// Register the /register endpoint
	http.HandleFunc("/register", registerHandler)

	// Start the HTTP server
	logrus.Info("Starting HTTP server")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		logrus.Fatal(&ErrorWithContext{
			Context: "starting HTTP server",
			Err:     err,
		})
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

	ip, err := parseRequest(r)
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "parsing request", "error": err}).Error("Error occurred while parsing request")
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Register the satellite
	logrus.WithField("ip", ip).Info("Registering satellite")
	registerSatellite(ip)

	// Update the Prometheus configuration
	if err := updatePrometheusConfiguration(ip); err != nil {
		logrus.WithFields(logrus.Fields{"context": "updating Prometheus configuration", "error": err}).Error("Error occurred while updating Prometheus configuration")
		return
	}

	// Respond with a success message
	fmt.Fprintln(w, "Registered new satellite:", ip)
}

// parseRequest parses the request body and returns the IP address.
func parseRequest(r *http.Request) (string, error) {
	var data map[string]string
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		logrus.WithFields(logrus.Fields{"context": "decoding request body", "error": err}).Error("Error occurred while decoding request body")
		return "", &ErrorWithContext{
			Context: "decoding request body",
			Err:     err,
		}
	}

	// Get the IP address from the request
	ip, ok := data["ip"]
	if !ok {
		logrus.WithFields(logrus.Fields{"context": "getting IP from request"}).Error("IP not provided in request")
		return "", &ErrorWithContext{
			Context: "IP not provided",
			Err:     fmt.Errorf("no IP in request"),
		}
	}

	return ip, nil
}

// registerSatellite adds the given IP address to the map of registered satellites.
func registerSatellite(ip string) {
	mu.Lock()
	defer mu.Unlock()
	satellites[ip] = true
}

// updatePrometheusConfiguration updates the Prometheus configuration to scrape metrics from the new satellite.
func updatePrometheusConfiguration(ip string) error {
	logrus.WithField("ip", ip).Info("Updating Prometheus configuration")

	// Read the existing configuration
	config, err := os.ReadFile("./docker/prometheus.yml")
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "reading Prometheus configuration", "error": err}).Error("Error occurred while reading Prometheus configuration")
		return &ErrorWithContext{
			Context: "reading Prometheus configuration",
			Err:     err,
		}
	}

	// Append the new satellite's IP address
	config = append(config, []byte("\n  - "+ip+":8080")...)

	// Write the updated configuration back to the file
	if err := os.WriteFile("./docker/prometheus.yml", config, 0644); err != nil {
		logrus.WithFields(logrus.Fields{"context": "writing Prometheus configuration", "error": err}).Error("Error occurred while writing Prometheus configuration")
		return &ErrorWithContext{
			Context: "writing Prometheus configuration",
			Err:     err,
		}
	}

	// Reload the Prometheus configuration
	resp, err := http.Post("http://35.87.31.126:9090/-/reload", "application/json", nil)
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "reloading Prometheus configuration", "error": err}).Error("Error occurred while reloading Prometheus configuration")
		return &ErrorWithContext{
			Context: "reloading Prometheus configuration",
			Err:     err,
		}
	}
	defer resp.Body.Close()

	// Check the response status
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			logrus.WithFields(logrus.Fields{"context": "reading response body", "error": err}).Error("Error occurred while reading response body")
			return &ErrorWithContext{
				Context: "reading response body",
				Err:     err,
			}
		}
		logrus.WithFields(logrus.Fields{"context": "reloading Prometheus configuration", "error": fmt.Sprintf("failed to reload configuration: %s", body)}).Error("Error occurred while reloading Prometheus configuration")
		return &ErrorWithContext{
			Context: "reloading Prometheus configuration",
			Err:     fmt.Errorf("failed to reload configuration: %s", body),
		}
	}

	return nil
}
