package utils

import (
	"crypto/tls"
	"fmt"
	"github.com/armon/go-socks5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"net"
	"net/http"
)

// Check the type of the IP address (IPv4 or IPv6)
func CheckIPType(ip string) string {
	host, _, err := net.SplitHostPort(ip)
	if err != nil {
		return "Unknown"
	}

	parsedIP := net.ParseIP(host)
	if parsedIP.To4() != nil {
		return "IPv4"
	} else if len(parsedIP) == net.IPv6len {
		return "IPv6"
	}
	return "Unknown"
}

// Define a listener that logs each accepted connection
type loggingListener struct {
	net.Listener
}

func (ll *loggingListener) Accept() (net.Conn, error) {
	conn, err := ll.Listener.Accept()
	if err != nil {
		return nil, err
	}

	logrus.WithFields(logrus.Fields{
		"event": "connection_accept",
		"addr":  conn.RemoteAddr().String(),
	}).Info("Accepted new connection")

	return conn, nil
}

// Set up SOCKS5 server and return reference to it
func SetupSocks5Server() *socks5.Server {
	conf := &socks5.Config{}
	socksServer, err := socks5.New(conf)
	if err != nil {
		logrus.Fatal(err)
	}
	return socksServer
}

// Get the IP address of the eth0 interface
func GetEth0IP() net.IP {
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		logrus.Fatal(err)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		logrus.Fatal(err)
	}
	eth0IP, _, err := net.ParseCIDR(addrs[0].String())
	if err != nil {
		logrus.Fatal(err)
	}
	return eth0IP
}

// Start a goroutine to listen for incoming connections
func ListenForConnections(socksServer *socks5.Server, eth0IP net.IP) {
	go func() {
		address := fmt.Sprintf("%s:1080", eth0IP.String())
		logrus.Info("Listening for incoming connections on ", address)
		listener, err := net.Listen("tcp", address)
		if err != nil {
			logrus.Fatal(err)
		}
		if err := socksServer.Serve(&loggingListener{listener}); err != nil {
			logrus.Error(err)
		}
	}()
}

// Set up HTTP server for metrics and return reference to it
func SetupHTTPServer() *http.Server {
	// Expose metrics over HTTPS
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Welcome to the server!")
	})
	http.Handle("/metrics", promhttp.Handler())
	httpServer := &http.Server{
		Addr:      ":8081",
		TLSConfig: &tls.Config{},
	}
	return httpServer
}

// Start HTTP server
func StartHTTPServer(httpServer *http.Server) {
	if err := httpServer.ListenAndServeTLS("cert.pem", "key.pem"); err != nil {
		logrus.Fatal(err)
	}
}

// GetPublicIP returns the public IP of the machine
func GetPublicIP() (string, error) {
	resp, err := http.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// GetIPv6 returns the IPv6 of the eth0 network interface
func GetIPv6() (string, error) {
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		return "", err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
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
		return 0, err
	}
	return cpuPercent[0], nil
}

// GetCurrentMemoryUsage returns the current memory usage
func GetCurrentMemoryUsage() (float64, error) {
	memStat, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return memStat.UsedPercent, nil
}
