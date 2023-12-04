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

// Dial dials out using the satellite's global unicast IPv6 address.
func (d *SimpleDialer) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	ipv6Addr, err := getIPv6Address()
	if err != nil {
		logrus.WithError(err).Error("Failed to get IPv6 address for outbound connections")
		return nil, err
	}

	localAddr := &net.TCPAddr{IP: ipv6Addr, Port: 0} // Port 0 means any available port
	dialer := &net.Dialer{
		LocalAddr: localAddr,
	}

	logrus.WithFields(logrus.Fields{
		"localIPv6": ipv6Addr,
		"network":   network,
		"address":   address,
	}).Info("Dialing out using IPv6")

	conn, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		logrus.WithError(err).Error("Failed to dial using IPv6 address")
		return nil, err
	}

	logrus.WithFields(logrus.Fields{
		"localIPv6":  ipv6Addr,
		"localPort":  conn.LocalAddr().(*net.TCPAddr).Port,
		"remoteAddr": conn.RemoteAddr().String(),
	}).Info("Successfully dialed using IPv6")

	return conn, nil
}

func getZeroTierIP() (string, error) {
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

	return "", errors.New("ZeroTier IP not found")
}

func Run() {
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetOutput(os.Stdout)
	logrus.Info("Starting main function")

	socksServer, err := utils.SetupSocks5Server()
	if err != nil {
		logrus.WithFields(logrus.Fields{"context": "setting up SOCKS5 server"}).Fatal(err)
	}

	dialer := &SimpleDialer{}
	manager := proxy.NewConnectionManager(dialer)

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
}

func handleTerminationSignals(httpServer *http.Server, listener net.Listener) {
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
}

func registerWithUXServer(uxServerIP string) error {
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

	return fmt.Errorf("registration failed after 3 attempts")
}

// getIPv6Address retrieves the preferred global unicast IPv6 address of the system.
func getIPv6Address() (net.IP, error) {
	logrus.Info("Retrieving global unicast IPv6 address for eth0")
	ifaces, err := net.Interfaces()
	if err != nil {
		logrus.WithError(err).Error("Failed to get network interfaces")
		return nil, err
	}

	var eth0IPv6 net.IP
	for _, iface := range ifaces {
		logrus.WithField("interface", iface.Name).Debug("Checking interface")

		if iface.Name != "eth0" {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			logrus.WithError(err).Error("Failed to get addresses for interface eth0")
			continue
		}

		for _, addr := range addrs {
			logrus.WithField("address", addr.String()).Debug("Found address for eth0")

			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP
			if ip.To4() != nil {
				logrus.WithField("ipv4", ip.String()).Debug("Ignoring IPv4 address for eth0")
				continue
			}

			if ip.IsGlobalUnicast() {
				logrus.WithField("ipv6", ip.String()).Debug("Found global unicast IPv6 address for eth0")
				eth0IPv6 = ip
			}
		}
	}

	if eth0IPv6 == nil {
		return nil, errors.New("eth0 interface has no global unicast IPv6 address")
	}

	logrus.WithField("selectedIPv6", eth0IPv6.String()).Info("Selected IPv6 address for eth0")
	return eth0IPv6, nil
}
