package ux

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"github.com/sirupsen/logrus"
)

type ErrorWithContext struct {
	Context string
	Err     error
}

func (e *ErrorWithContext) Error() string {
	return fmt.Sprintf("%s: %v", e.Context, e.Err)
}

var (
	satellites = make(map[string]bool)
	mu         sync.Mutex
)

func LaunchPrometheus() {
	cmd := exec.Command("prometheus", "--config.file=./prometheus.yml", "--web.enable-lifecycle")
	err := cmd.Start()
	if err != nil {
		logrus.Fatal(&ErrorWithContext{
			Context: "starting Prometheus server",
			Err:     err,
		})
	}
	logrus.Info("Prometheus server started")
}

func Run() {
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.InfoLevel)

	// Launch Prometheus server
	LaunchPrometheus()

	http.HandleFunc("/register", registerHandler)

	logrus.Info("Starting HTTP server")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		logrus.Fatal(&ErrorWithContext{
			Context: "starting HTTP server",
			Err:     err,
		})
	}
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	logrus.Info("Received registration request")

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

	logrus.WithField("ip", ip).Info("Registering satellite")
	registerSatellite(ip)

	if err := updatePrometheusConfiguration(ip); err != nil {
		logrus.WithFields(logrus.Fields{"context": "updating Prometheus configuration", "error": err}).Error("Error occurred while updating Prometheus configuration")
		return
	}

	fmt.Fprintln(w, "Registered new satellite:", ip)
}

func parseRequest(r *http.Request) (string, error) {
	var data map[string]string
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		logrus.WithFields(logrus.Fields{"context": "decoding request body", "error": err}).Error("Error occurred while decoding request body")
		return "", &ErrorWithContext{
			Context: "decoding request body",
			Err:     err,
		}
	}

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

func registerSatellite(ip string) {
	mu.Lock()
	defer mu.Unlock()
	satellites[ip] = true
}

func updatePrometheusConfiguration(ip string) error {
	logrus.WithField("ip", ip).Info("Updating Prometheus configuration")

	config := []byte(`
global:
  scrape_interval:     1s
  evaluation_interval: 1s

scrape_configs:
  - job_name: 'prometheus'
    static_configs:
    - targets: ['` + ip + `:8080']
`)

	if err := os.WriteFile("./prometheus.yml", config, 0644); err != nil {
		logrus.WithFields(logrus.Fields{"context": "writing Prometheus configuration", "error": err}).Error("Error occurred while writing Prometheus configuration")
		return &ErrorWithContext{
			Context: "writing Prometheus configuration",
			Err:     err,
		}
	}

	resp, err := http.Post("http://35.87.31.126:9090/-/reload", "application/json", nil)
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "reloading Prometheus configuration", "error": err}).Error("Error occurred while reloading Prometheus configuration")
		return &ErrorWithContext{
			Context: "reloading Prometheus configuration",
			Err:     err,
		}
	}
	defer resp.Body.Close()

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
