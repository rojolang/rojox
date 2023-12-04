package utils

import (
	"errors"
	"fmt"
	"github.com/armon/go-socks5"
	"github.com/rojolang/rojox/proxy"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"net/http"
)

// ListenForConnections starts a goroutine that listens for incoming connections on the specified address.
// It returns the listener and an error if there was an issue setting it up.
func ListenForConnections(socksServer *socks5.Server, eth0IP net.IP, manager *proxy.ConnectionManager) (net.Listener, error) {
	address := fmt.Sprintf("%s:9050", eth0IP.String())
	logrus.Info("Listening for incoming connections on ", address)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "listening for connections"}).Error(err)
		return nil, err
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if opErr, ok := err.(*net.OpError); ok && opErr.Op == "accept" {
					logrus.WithFields(logrus.Fields{"context": "accepting connection"}).Info("Listener closed")
				} else {
					logrus.WithFields(logrus.Fields{"context": "accepting connection"}).Error(err)
				}
				return
			}
			manager.AcceptConnection() // Increment the totalConnections counter
			logrus.WithFields(logrus.Fields{
				"event":       "connection_accept",
				"local_addr":  conn.LocalAddr().String(),
				"remote_addr": conn.RemoteAddr().String(),
			}).Info("Accepted new connection")

			go manager.HandleConnection(socksServer, conn) // Call the HandleConnection method of the manager
		}
	}()

	return listener, nil
}

// CheckIPType checks the type of the given IP address.
func CheckIPType(ip string) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP.To4() != nil {
		return "IPv4"
	} else if parsedIP.To16() != nil {
		return "IPv6"
	}
	return "Unknown"
}

// GetCurrentCPUUsage retrieves the current CPU usage percentage.
func GetCurrentCPUUsage() (float64, error) {
	percentages, err := cpu.Percent(0, false)
	if err != nil {
		return 0, err
	}
	if len(percentages) > 0 {
		return percentages[0], nil // Return the first CPU's usage if available
	}
	return 0, errors.New("CPU usage data not available")
}

// GetCurrentMemoryUsage retrieves the current memory usage percentage.
func GetCurrentMemoryUsage() (float64, error) {
	vmem, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return vmem.UsedPercent, nil
}

// GetPublicIP gets the public IP address of the server.
// It returns the IP address or an error if there was an issue getting it.
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
// It returns the IPv6 address or an error if there was an issue getting it.
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
