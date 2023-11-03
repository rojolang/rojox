package utils

import (
	"crypto/tls"
	"fmt"
	"github.com/armon/go-socks5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"net/http"
	"os"
)

// CheckIPType checks the type of the IP address (IPv4 or IPv6)
// CheckIPType checks the type of the IP address (IPv4 or IPv6)
func CheckIPType(ip string) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		logrus.Error("Invalid IP address: ", ip)
		return "Unknown"
	}
	if parsedIP.To4() != nil {
		return "IPv4"
	} else if len(parsedIP) == net.IPv6len {
		return "IPv6"
	}
	return "Unknown"
}

// loggingListener defines a listener that logs each accepted connection
type loggingListener struct {
	net.Listener
}

func (ll *loggingListener) Accept() (net.Conn, error) {
	conn, err := ll.Listener.Accept()
	if err != nil {
		logrus.Error("Failed to accept connection: ", err)
		return nil, err
	}

	logrus.WithFields(logrus.Fields{
		"event": "connection_accept",
		"addr":  conn.RemoteAddr().String(),
	}).Info("Accepted new connection")

	return conn, nil
}

// SetupSocks5Server sets up SOCKS5 server and return reference to it
func SetupSocks5Server() (*socks5.Server, error) {
	conf := &socks5.Config{}
	socksServer, err := socks5.New(conf)
	if err != nil {
		logrus.Error("Failed to create new SOCKS5 server: ", err)
		return nil, err
	}
	return socksServer, nil
}

// GetEth0IP gets the IP address of the eth0 interface
func GetEth0IP() (net.IP, error) {
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		logrus.Error("Failed to get eth0 interface: ", err)
		return nil, err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		logrus.Error("Failed to get addresses for eth0: ", err)
		return nil, err
	}
	eth0IP, _, err := net.ParseCIDR(addrs[0].String())
	if err != nil {
		logrus.Error("Failed to parse CIDR for eth0: ", err)
		return nil, err
	}
	return eth0IP, nil
}

// ListenForConnections starts a goroutine to listen for incoming connections
func ListenForConnections(socksServer *socks5.Server, eth0IP net.IP) {
	go func() {
		address := fmt.Sprintf("%s:1080", eth0IP.String())
		logrus.Info("Listening for incoming connections on ", address)
		listener, err := net.Listen("tcp", address)
		if err != nil {
			logrus.Error("Failed to listen on address: ", err)
			return
		}
		if err := socksServer.Serve(&loggingListener{listener}); err != nil {
			logrus.Error("Failed to serve SOCKS5 server: ", err)
		}
	}()
}

// SetupHTTPServer sets up HTTP server for metrics and return reference to it
func SetupHTTPServer() (*http.Server, error) {
	// Expose metrics over HTTPS
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := fmt.Fprintln(w, "Welcome to the server!")
		if err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
			logrus.Error("Failed to write response: ", err)
		}
	})
	http.Handle("/metrics", promhttp.Handler())

	// Check if TLS certificates exist
	_, err := os.Stat("cert.pem")
	if err != nil {
		logrus.Error("Failed to find cert.pem: ", err)
		return nil, err
	}
	_, err = os.Stat("key.pem")
	if err != nil {
		logrus.Error("Failed to find key.pem: ", err)
		return nil, err
	}

	httpServer := &http.Server{
		Addr:      ":8081",
		TLSConfig: &tls.Config{},
	}
	return httpServer, nil
}

// StartHTTPServer starts HTTP server
func StartHTTPServer(httpServer *http.Server) error {
	if err := httpServer.ListenAndServeTLS("cert.pem", "key.pem"); err != nil {
		logrus.Error("Failed to start HTTP server: ", err)
		return err
	}
	return nil
}

// GetPublicIP returns the public IP of the machine
func GetPublicIP() (string, error) {
	resp, err := http.Get("https://api.ipify.org")
	if err != nil {
		logrus.Error("Failed to get public IP: ", err)
		return "", err
	}

	defer func() {
		cerr := resp.Body.Close()
		if cerr != nil {
			logrus.Error("Failed to close response body: ", cerr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Error("Failed to read response body: ", err)
		return "", err
	}

	return string(body), nil
}

// GetIPv6 returns the IPv6 of the eth0 network interface
func GetIPv6() (string, error) {
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		logrus.Error("Failed to get eth0 interface: ", err)
		return "", err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		logrus.Error("Failed to get addresses for eth0: ", err)
		return "", err
	}

	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			logrus.Error("Failed to parse CIDR for eth0: ", err)
			return "", err
		}
		if ip.To4() == nil && len(ip) == net.IPv6len {
			// This is an IPv6 address
			return ip.String(), nil
		}
	}

	return "", fmt.Errorf("IPv6 address not found for eth0")
}

// GetCurrentCPUUsage returns the current CPU usage
func GetCurrentCPUUsage() (float64, error) {
	cpuPercent, err := cpu.Percent(0, false)
	if err != nil {
		logrus.Error("Failed to get CPU usage: ", err)
		return 0, err
	}
	return cpuPercent[0], nil
}

// GetCurrentMemoryUsage returns the current memory usage
func GetCurrentMemoryUsage() (float64, error) {
	memStat, err := mem.VirtualMemory()
	if err != nil {
		logrus.Error("Failed to get memory usage: ", err)
		return 0, err
	}
	return memStat.UsedPercent, nil
}
