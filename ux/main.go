package ux

import (
	"encoding/json"
	"fmt"
	"github.com/rojolang/rojox/server"
	"github.com/sirupsen/logrus"
	"net"
	"net/http"
	"os"
	"time"
)

type ErrorWithContext struct {
	Context string
	Err     error
}

func (e *ErrorWithContext) Error() string {
	return fmt.Sprintf("%s: %v", e.Context, e.Err)
}

func Run() {
	logrus.SetOutput(os.Stdout)
	logrus.SetLevel(logrus.InfoLevel)

	lb := server.NewLoadBalancer()

	http.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		registerHandler(w, r, lb) // Pass the LoadBalancer instance to the handler
	})

	go startListener(lb)

	logrus.Info("Starting HTTP server")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		logrus.Fatal(&ErrorWithContext{
			Context: "starting HTTP server",
			Err:     err,
		})
	}
}

func startListener(lb *server.LoadBalancer) {
	for {
		logrus.Info("Listening for incoming connections")
		listener, err := net.Listen("tcp", ":9050")
		if err != nil {
			logrus.WithFields(logrus.Fields{"context": "listening for connections"}).Error(err)
			return
		}
		defer listener.Close()

		for {
			conn, err := listener.Accept()
			if err != nil {
				logrus.WithFields(logrus.Fields{"context": "accepting connection"}).Error(err)
				break
			}
			go lb.HandleConnection(conn)
		}
		time.Sleep(1 * time.Second) // If the listener breaks, wait a second before retrying
	}
}

func registerHandler(w http.ResponseWriter, r *http.Request, lb *server.LoadBalancer) {
	logrus.Info("Received registration request from ", r.RemoteAddr)

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
	lb.RegisterSatellite(ip) // Register satellite with the LoadBalancer
	fmt.Fprintln(w, "Registered new satellite:", ip)
}

func parseRequest(r *http.Request) (string, error) {
	var data map[string]string
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		logrus.WithFields(logrus.Fields{"context": "decoding request body", "error": err}).Error("Error occurred while decoding request body")
		return "", err
	}

	ip, ok := data["ip"]
	if !ok {
		logrus.WithFields(logrus.Fields{"context": "getting IP from request"}).Error("IP not provided in request")
		return "", fmt.Errorf("IP not provided in request")
	}

	logrus.WithField("ip", ip).Info("Parsed IP from request")
	return ip, nil
}
