package utils

import (
	"crypto/tls"
	"fmt"
	"github.com/armon/go-socks5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rojolang/rojox/proxy"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"net/http"
	"os"
)

// CheckIPType checks the type of the given IP address.
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

// SetupSocks5Server sets up a new SOCKS5 server.
func SetupSocks5Server() (*socks5.Server, error) {
	conf := &socks5.Config{}
	socksServer, err := socks5.New(conf)
	if err != nil {
		logrus.Error("Failed to create new SOCKS5 server: ", err)
		return nil, err
	}
	return socksServer, nil
}

// GetEth0IP gets the IP address for the eth0 network interface.
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
func ListenForConnections(socksServer *socks5.Server, eth0IP net.IP, manager *proxy.ConnectionManager) {
	go func() {
		address := fmt.Sprintf("%s:1080", eth0IP.String())
		logrus.Info("Listening for incoming connections on ", address)
		listener, err := net.Listen("tcp", address)
		if err != nil {
			logrus.Error("Failed to listen on address: ", err)
			return
		}
		for {
			conn, err := listener.Accept()
			if err != nil {
				logrus.Error("Failed to accept connection: ", err)
				return
			}
			logrus.WithFields(logrus.Fields{
				"event": "connection_accept",
				"addr":  conn.RemoteAddr().String(),
			}).Info("Accepted new connection")

			go func() {
				if err := socksServer.ServeConn(conn); err != nil {
					logrus.Error("Failed to serve connection: ", err)
				}
				manager.Close(conn)
			}()
		}
	}()
}

func SetupHTTPServer() (*http.Server, error) {
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

// StartHTTPServer starts the given HTTP server.
func StartHTTPServer(httpServer *http.Server) error {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := fmt.Fprintln(w, "Welcome to the server!")
		if err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
			logrus.Error("Failed to write response: ", err)
		}
	})
	http.Handle("/metrics", promhttp.Handler())

	if err := httpServer.ListenAndServeTLS("cert.pem", "key.pem"); err != nil {
		logrus.Error("Failed to start HTTP server: ", err)
		return err
	}
	return nil
}

// GetPublicIP gets the public IP address of the server.
func GetPublicIP() (string, error) {
	resp, err := http.Get("https://api.ipify.org")
	if err != nil {
		logrus.Error("Failed to get public IP: ", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Error("Failed to read response body: ", err)
		return "", err
	}

	return string(body), nil
}

// GetIPv6 gets the IPv6 address for the eth0 network interface.
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

// GetCurrentCPUUsage gets the current CPU usage.
func GetCurrentCPUUsage() (float64, error) {
	cpuPercent, err := cpu.Percent(0, false)
	if err != nil {
		logrus.Error("Failed to get CPU usage: ", err)
		return 0, err
	}
	return cpuPercent[0], nil
}

// GetCurrentMemoryUsage gets the current memory usage.
func GetCurrentMemoryUsage() (float64, error) {
	memStat, err := mem.VirtualMemory()
	if err != nil {
		logrus.Error("Failed to get memory usage: ", err)
		return 0, err
	}
	return memStat.UsedPercent, nil
}
