package ux

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/rojolang/rojox/server" // replace with your actual project path
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

func Run() {
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.InfoLevel) // Set log level to Info

	http.HandleFunc("/register", registerHandler)

	// Create a new LoadBalancer
	lb := server.NewLoadBalancer()

	// Start a goroutine to listen for incoming connections
	go func() {
		// Add a 5-second delay
		time.Sleep(5 * time.Second)

		listener, err := net.Listen("tcp", ":1080")
		if err != nil {
			logrus.WithFields(logrus.Fields{"context": "listening for connections"}).Error(err)
			return
		}
		defer listener.Close()

		for {
			conn, err := listener.Accept()
			if err != nil {
				logrus.WithFields(logrus.Fields{"context": "accepting connection"}).Error(err)
				return
			}
			go lb.HandleConnection(conn)
		}
	}()

	logrus.Info("Starting HTTP server")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		logrus.Fatal(&ErrorWithContext{
			Context: "starting HTTP server",
			Err:     err,
		})
	}
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	logrus.Info("Received registration request") // Changed to Info

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

	logrus.WithField("ip", ip).Info("Registering satellite") // Changed to Info
	registerSatellite(ip)

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
	logrus.WithField("ip", ip).Info("Registered satellite") // Changed Debug to Info
}
