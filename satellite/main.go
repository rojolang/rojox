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

const UXServerIP = "http://34.209.231.131:8080/register"

type SimpleDialer struct{}

// Dial creates a network connection using the specified network, address, and context.
// It prefers IPv6 and falls back to IPv4 on the usb0 interface for outgoing connections.
func (d *SimpleDialer) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	logrus.Debug("Entering SimpleDialer.Dial method")

	// Create a dialer without specifying LocalAddr to use the system's default routing
	dialer := &net.Dialer{}

	// Dial out using the system's default routing
	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		logrus.WithError(err).Error("Failed to dial")
		return nil, err
	}

	logrus.WithFields(logrus.Fields{
		"localAddr":  conn.LocalAddr().String(),
		"remoteAddr": conn.RemoteAddr().String(),
	}).Info("Successfully established connection")
	logrus.Debug("Exiting SimpleDialer.Dial method")
	return conn, nil
}

func dialWithLocalAddr(ctx context.Context, network, address string, localIP net.IP) (net.Conn, error) {
	localAddr := &net.TCPAddr{IP: localIP, Port: 0}
	dialer := &net.Dialer{LocalAddr: localAddr}
	return dialer.DialContext(ctx, network, address)
}

func getZeroTierIP() (string, error) {
	logrus.Debug("Satellite getZeroTierIP function started")
	cmd := exec.Command("zerotier-cli", "listnetworks")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run() // Use Run instead of Output to capture both stdout and stderr
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"stderr": stderr.String(),
			"stdout": stdout.String(),
		}).Error("Error executing zerotier-cli listnetworks")
		return "", err
	}

	output := stdout.String()
	lines := strings.Split(output, "\n")
	logrus.Info("ZeroTier networks: ", lines)

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 8 && fields[2] == "fada62b0151e0f56" {
			ipWithMask := fields[8]
			ip := strings.Split(ipWithMask, "/")[0]
			logrus.Info("ZeroTier IP found: ", ip)
			return ip, nil
		}
	}
	logrus.Debug("Satellite getZeroTierIP function finished")
	return "", errors.New("ZeroTier IP not found")
}

// getUSB0Addresses returns the IPv6 and IPv4 addresses of the usb0 interface.
func getUSB0Addresses() (ipv6Addr, ipv4Addr net.IP) {
	ifaces, err := net.Interfaces()
	if err != nil {
		logrus.WithError(err).Error("Failed to get network interfaces")
		return nil, nil
	}

	for _, iface := range ifaces {
		if iface.Name != "usb0" {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			logrus.WithError(err).WithField("interface", iface.Name).Error("Failed to get addresses for interface")
			return nil, nil
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP
			if ip.To4() != nil {
				ipv4Addr = ip
			} else if ip.To16() != nil && ip.IsGlobalUnicast() {
				ipv6Addr = ip
			}
		}
	}

	return ipv6Addr, ipv4Addr
}

func Run() {
	logrus.Debug("Satellite Run function started")
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetOutput(os.Stdout)
	logrus.Info("Starting satellite main function")

	// Create a new SimpleDialer instance which will be used by the ConnectionManager
	dialer := &SimpleDialer{}
	// Create a new ConnectionManager instance providing the SimpleDialer
	manager := proxy.NewConnectionManager(dialer)

	// Setup the SOCKS5 server with the custom Dial function from our SimpleDialer
	socksServer, err := utils.SetupSocks5Server()
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "setting up SOCKS5 server"}).Fatal(err)
	}

	// Fetch ZeroTier IP instead of eth0 IP
	zeroTierIPString, err := getZeroTierIP()
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "getting ZeroTier IP"}).Fatal(err)
	}

	// Parse the ZeroTier IP string into net.IP type
	zeroTierIP := net.ParseIP(zeroTierIPString)
	if zeroTierIP == nil {
		logrus.WithFields(logrus.Fields{"context": "parsing ZeroTier IP"}).Fatal("Invalid ZeroTier IP")
	}

	if err := registerWithUXServer(UXServerIP); err != nil {
		logrus.WithFields(logrus.Fields{"context": "registering with UX server"}).Fatal(err)
	}

	// Use ZeroTier IP when listening for connections
	listener, err := utils.ListenForConnections(socksServer, zeroTierIP, manager)
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "listening for connections"}).Fatal(err)
	}

	httpServer, err := utils.SetupHTTPServer()
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "setting up HTTP server"}).Fatal(err)
	}

	http.Handle("/metrics-satellite", promhttp.Handler())

	logrus.Info("Handling termination signals")
	handleTerminationSignals(httpServer, listener)
	logrus.Info("Termination signals handled")

	logrus.Info("Starting HTTP server")
	go func() {
		if err := utils.StartHTTPServer(httpServer); err != nil {
			logrus.WithFields(logrus.Fields{"context": "starting HTTP server"}).Fatal(err)
		}
	}()
	logrus.Info("HTTP server started")

	logrus.Info("Starting PrintStats goroutine")
	go stats.PrintStats(manager)
	logrus.Info("PrintStats goroutine started")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	if err := httpServer.Shutdown(context.Background()); err != nil {
		logrus.WithFields(logrus.Fields{"context": "shutting down HTTP server"}).Error(err)
	}
	logrus.Debug("Satellite Run function finished")
}

func handleTerminationSignals(httpServer *http.Server, listener net.Listener) {
	logrus.Debug("Satellite handleTerminationSignals function started")
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-quit

		logrus.Info("Received termination signal")

		if err := httpServer.Shutdown(context.Background()); err != nil {
			logrus.WithFields(logrus.Fields{"context": "shutting down HTTP server"}).Error(err)
		}

		if err := listener.Close(); err != nil {
			logrus.WithFields(logrus.Fields{"context": "closing SOCKS5 server listener"}).Error(err)
		}
	}()
	logrus.Debug("Satellite handleTerminationSignals function finished")
}

func registerWithUXServer(uxServerIP string) error {
	logrus.Debug("Satellite registerWithUXServer function started")
	for i := 0; i < 3; i++ {
		ip, err := getZeroTierIP()
		if err != nil {
			logrus.WithField("context", "getting ZeroTier IP").Error(err)
			time.Sleep(1 * time.Second)
			continue
		}
		logrus.Info("Registering IP: ", ip)

		reqBody, err := json.Marshal(map[string]string{"ip": ip})
		if err != nil {
			logrus.WithField("context", "creating register request").Error(err)
			time.Sleep(1 * time.Second)
			continue
		}
		req, err := http.NewRequest("POST", uxServerIP, bytes.NewBuffer(reqBody))

		if err != nil {
			logrus.WithField("context", "creating new request").Error(err)
			time.Sleep(1 * time.Second)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		logrus.WithField("request", req).Info("Sending registration request")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logrus.WithField("context", "sending register request").Error(err)
			time.Sleep(1 * time.Second)
			continue
		}
		defer resp.Body.Close()

		logrus.WithField("response", resp).Info("Received registration response")

		if resp.StatusCode != http.StatusOK {
			err = fmt.Errorf("registration failed: status code %d", resp.StatusCode)
			logrus.WithField("context", "register response").Error(err)
			time.Sleep(1 * time.Second)
			continue
		}

		logrus.Info("Successfully registered with UX server")
		return nil
	}
	logrus.Debug("Satellite registerWithUXServer function finished")
	return fmt.Errorf("registration failed after 3 attempts")
}

// getIPv6Address retrieves the preferred global unicast IPv6 address of the system.
func getIPv6Address() (net.IP, error) {
	logrus.Debug("Satellite getIPv6Address function started")
	logrus.Info("Retrieving global unicast IPv6 address for usb0")
	iface, err := net.InterfaceByName("usb0")
	if err != nil {
		logrus.WithError(err).Error("Failed to get usb0 network interface")
		return nil, err
	}

	addrs, err := iface.Addrs()
	if err != nil {
		logrus.WithError(err).Error("Failed to get addresses for interface usb0")
		return nil, err
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		if ip.To4() == nil && ip.IsGlobalUnicast() {
			// Found a global unicast IPv6 address.
			logrus.WithField("ipv6", ip.String()).Info("Global unicast IPv6 address found for usb0")
			return ip, nil
		}
	}
	logrus.Debug("Satellite getIPv6Address function finished")
	return nil, errors.New("usb0 interface has no global unicast IPv6 address")
}

func getUSB0IPv6() (net.IP, error) {
	ifaces, err := net.Interfaces()
	logrus.Debug("Entering getUSB0IPv6 function")
	if err != nil {
		logrus.WithError(err).Error("Failed to get network interfaces")
		return nil, err
	}

	for _, iface := range ifaces {
		if iface.Name != "usb0" {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			logrus.WithError(err).WithField("interface", iface.Name).Error("Failed to get addresses for interface")
			return nil, err
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP
			if ip.To4() == nil && ip.IsGlobalUnicast() {
				logrus.WithFields(logrus.Fields{
					"interface": "usb0",
					"ipv6":      ip.String(),
				}).Info("IPv6 address found for usb0")
				return ip, nil
			}
		}
	}
	logrus.Debug("Exiting getUSB0IPv6 function")
	return nil, fmt.Errorf("usb0 interface not found or has no global unicast IPv6 address")
}

func getUSB0IPv4() (net.IP, error) {
	ifaces, err := net.Interfaces()
	logrus.Debug("Entering getUSB0IPv4 function")
	if err != nil {
		logrus.WithError(err).Error("Failed to get network interfaces")
		return nil, err
	}

	for _, iface := range ifaces {
		if iface.Name != "usb0" {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			logrus.WithError(err).WithField("interface", iface.Name).Error("Failed to get addresses for interface")
			return nil, err
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP
			// Check if the IP is an IPv4 address.
			if ipv4 := ip.To4(); ipv4 != nil {
				logrus.WithFields(logrus.Fields{
					"interface": "usb0",
					"ipv4":      ipv4.String(),
				}).Info("IPv4 address found for usb0")
				return ipv4, nil // Return the IPv4 address.
			}
		}
	}
	logrus.Debug("Exiting getUSB0IPv4 function")
	return nil, fmt.Errorf("usb0 interface not found or has no IPv4 address")
}
