// Package proxy provides functionality to manage and monitor network connections.
package proxy

import (
	"context"
	"fmt"
	"github.com/armon/go-socks5"
	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Dialer defines the interface for network dialing.
type Dialer interface {
	Dial(ctx context.Context, network, address string) (net.Conn, error)
}

// SimpleDialer implements the Dialer interface, providing methods to dial network connections.
type SimpleDialer struct{}

func (d *SimpleDialer) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		zap.L().Error("Failed to split network address", zap.Error(err))
		return nil, fmt.Errorf("failed to split network address: %w", err)
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		zap.L().Error("Failed to look up IP for host", zap.Error(err))
		return nil, fmt.Errorf("failed to look up IP for host: %w", err)
	}

	var dialAddr string
	var conn net.Conn
	dialer := &net.Dialer{}

	// Iterate over the IPs and select the first IPv6 address found.
	foundIPv6 := false
	for _, ip := range ips {
		if ip.To4() == nil && ip.IsGlobalUnicast() {
			foundIPv6 = true
			dialAddr = net.JoinHostPort(ip.String(), port)
			zap.L().Info("Attempting to dial IPv6 address", zap.String("address", dialAddr))
			conn, err = dialer.DialContext(ctx, "tcp6", dialAddr)
			if err == nil {
				zap.L().Info("Successfully dialed IPv6 address", zap.String("address", dialAddr))
				return conn, nil
			}
			zap.L().Error("Failed to dial IPv6", zap.Error(err), zap.String("address", dialAddr))
		}
	}

	if !foundIPv6 {
		zap.L().Info("No IPv6 address found, falling back to IPv4", zap.String("host", host))
	}

	// If no IPv6 addresses are available or all attempts fail, fall back to IPv4.
	dialAddr = net.JoinHostPort(host, port)
	zap.L().Info("Attempting to dial IPv4 address", zap.String("address", dialAddr))
	conn, err = dialer.DialContext(ctx, "tcp4", dialAddr)
	if err != nil {
		zap.L().Error("Failed to dial IPv4", zap.Error(err), zap.String("address", dialAddr))
		return nil, fmt.Errorf("failed to dial IPv4: %w", err)
	}

	zap.L().Info("Successfully dialed IPv4 address", zap.String("address", dialAddr))
	return conn, nil
}

// ConnectionManager manages and monitors network connections.
type ConnectionManager struct {
	dialer                     Dialer
	totalRequests              int64
	totalFailed                int64
	totalConnections           int64
	totalSuccessfulConnections int64
	totalFailedConnections     int64
	maxConcurrentConnections   int64
	currentConnections         int64
	startTime                  time.Time
	conns                      map[string]net.Conn
	connMutex                  sync.Mutex
	bytesSent                  int64
	bytesReceived              int64
	lastRequestDuration        time.Duration // Field to store the duration of the last request
}

// NewConnectionManager creates a new ConnectionManager with the provided Dialer.
func NewConnectionManager(dialer Dialer) *ConnectionManager {
	logrus.Info("Creating new ConnectionManager")
	return &ConnectionManager{
		dialer:    dialer,
		startTime: time.Now(),
		conns:     make(map[string]net.Conn),
	}
}

// Connect establishes a new network connection using the provided network and address.
func (m *ConnectionManager) Connect(ctx context.Context, network, address string) (net.Conn, error) {
	logrus.WithFields(logrus.Fields{"network": network, "address": address}).Info("Connecting...")
	logrus.Debug("Entering ConnectionManager.Connect method")
	conn, err := m.dialer.Dial(ctx, network, address)
	if err != nil {
		atomic.AddInt64(&m.totalFailed, 1)
		logrus.WithFields(logrus.Fields{
			"network": network,
			"address": address,
		}).Error("Failed to create connection")
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	atomic.AddInt64(&m.totalRequests, 1)
	atomic.AddInt64(&m.totalSuccessfulConnections, 1)
	m.connMutex.Lock()
	m.conns[conn.RemoteAddr().String()] = conn
	m.connMutex.Unlock()
	logrus.WithFields(logrus.Fields{
		"network": network,
		"address": address,
	}).Info("Successfully created connection")
	logrus.Debug("Exiting ConnectionManager.Connect method")
	return conn, nil
}

// isZeroTierIP checks if the given IP address belongs to the ZeroTier network or is the local IP.
func isZeroTierIP(ip string, conn net.Conn) bool {
	// This example assumes that the ZeroTier network uses the 10.243.0.0/16 range.
	// Replace with the actual IP range of your ZeroTier network.
	_, zeroTierNet, _ := net.ParseCIDR("10.243.0.0/16")

	// Check if the remote address is a ZeroTier IP or the local IP.
	if zeroTierNet.Contains(net.ParseIP(ip)) || ip == "10.0.127.101" {
		logrus.WithField("ip", ip).Info("ZeroTier IP detected")
		return true
	}

	// If not, check if the local address is a ZeroTier IP or the local IP.
	localIP, _, _ := net.SplitHostPort(conn.LocalAddr().String())
	if zeroTierNet.Contains(net.ParseIP(localIP)) || localIP == "10.0.127.101" {
		logrus.WithField("localIP", localIP).Info("Local ZeroTier IP detected")
		return true
	}

	return false
}

func (m *ConnectionManager) HandleConnection(socksServer *socks5.Server, conn net.Conn) {
	defer m.Close(conn)
	logrus.Debug("Entering ConnectionManager.HandleConnection method")
	ip, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"event":       "split_host_port_error",
			"local_addr":  conn.LocalAddr().String(),
			"remote_addr": conn.RemoteAddr().String(),
		}).Error("Failed to split host port")
		return
	}

	if !isZeroTierIP(ip, conn) {
		logrus.WithFields(logrus.Fields{
			"event":       "rejected_non_zerotier_ip",
			"remote_addr": conn.RemoteAddr().String(),
		}).Warn("Rejected connection from non-ZeroTier IP")
		return
	}

	logrus.WithFields(logrus.Fields{
		"event":       "accepted_zerotier_connection",
		"local_addr":  conn.LocalAddr().String(),
		"remote_addr": conn.RemoteAddr().String(),
		"zerotier_ip": ip,
	}).Info("Accepted connection from ZeroTier IP")

	// The socksServer.ServeConn method will handle the SOCKS5 protocol negotiation,
	// including forwarding the connection to the requested destination.
	// Ensure that the socksServer is configured to use our SimpleDialer for outbound connections.
	if err := socksServer.ServeConn(conn); err != nil {
		atomic.AddInt64(&m.totalFailedConnections, 1)
		logrus.WithFields(logrus.Fields{
			"event":       "serve_connection_error",
			"local_addr":  conn.LocalAddr().String(),
			"remote_addr": conn.RemoteAddr().String(),
		}).Error("Failed to serve connection")
	} else {
		atomic.AddInt64(&m.totalSuccessfulConnections, 1)
		atomic.AddInt64(&m.totalRequests, 1)
		logrus.WithFields(logrus.Fields{
			"event":       "serve_connection_success",
			"local_addr":  conn.LocalAddr().String(),
			"remote_addr": conn.RemoteAddr().String(),
		}).Info("Successfully served connection")
	}
	logrus.Debug("Exiting ConnectionManager.HandleConnection method")
}

// GetMaxConcurrentConnections returns the maximum number of concurrent connections that have been active at the same time.
func (m *ConnectionManager) GetMaxConcurrentConnections() int64 {
	maxConcurrentConnections := atomic.LoadInt64(&m.maxConcurrentConnections)
	logrus.WithField("maxConcurrentConnections", maxConcurrentConnections).Debug("Retrieved max concurrent connections")
	return maxConcurrentConnections
}

// GetTotalRequests returns the total number of requests made since the creation of the ConnectionManager.
func (m *ConnectionManager) GetTotalRequests() int64 {
	totalRequests := atomic.LoadInt64(&m.totalRequests)
	logrus.WithField("totalRequests", totalRequests).Debug("Retrieved total number of requests")
	return totalRequests
}

// GetTotalFailed returns the total number of failed connection attempts since the ConnectionManager was created.
func (m *ConnectionManager) GetTotalFailed() int64 {
	totalFailed := atomic.LoadInt64(&m.totalFailed)
	logrus.WithField("totalFailed", totalFailed).Debug("Retrieved total number of failed connections")
	return totalFailed
}

// AcceptConnection increments the connection counters and updates the maximum concurrent connections if the current value exceeds the previously recorded maximum.
func (m *ConnectionManager) AcceptConnection() {
	atomic.AddInt64(&m.totalConnections, 1)
	currentConnections := atomic.AddInt64(&m.currentConnections, 1)
	maxConcurrentConnections := atomic.LoadInt64(&m.maxConcurrentConnections)
	if currentConnections > maxConcurrentConnections {
		atomic.StoreInt64(&m.maxConcurrentConnections, currentConnections)
		logrus.WithField("newMaxConcurrentConnections", currentConnections).Debug("Updated max concurrent connections")
	}
	logrus.WithFields(logrus.Fields{
		"totalConnections":   atomic.LoadInt64(&m.totalConnections),
		"currentConnections": currentConnections,
		"maxConcurrentConns": maxConcurrentConnections,
	}).Debug("Accepted new connection")
}

// GetTotalConnections returns the total number of connections that have been accepted since the ConnectionManager was created.
func (m *ConnectionManager) GetTotalConnections() int64 {
	totalConnections := atomic.LoadInt64(&m.totalConnections)
	logrus.WithField("totalConnections", totalConnections).Debug("Retrieved total number of connections")
	return totalConnections
}

// GetUptime returns the duration since the ConnectionManager was created.
func (m *ConnectionManager) GetUptime() time.Duration {
	uptime := time.Since(m.startTime)
	logrus.WithField("uptime", uptime).Debug("Retrieved uptime of the ConnectionManager")
	return uptime
}

// Close terminates the given network connection and removes it from the manager's tracking.
func (m *ConnectionManager) Close(conn net.Conn) {
	m.connMutex.Lock()
	defer m.connMutex.Unlock()

	addr := conn.RemoteAddr().String()
	if _, ok := m.conns[addr]; ok {
		if err := conn.Close(); err != nil {
			logrus.WithField("address", addr).Error("Failed to close connection")
		} else {
			logrus.WithField("address", addr).Debug("Successfully closed connection")
			delete(m.conns, addr)
		}
	}
}

// GetTotalFailedConnections returns the total number of connections that have failed after being accepted.
func (m *ConnectionManager) GetTotalFailedConnections() int64 {
	totalFailedConnections := atomic.LoadInt64(&m.totalFailedConnections)
	logrus.WithField("totalFailedConnections", totalFailedConnections).Debug("Retrieved total number of failed connections")
	return totalFailedConnections
}

// GetTotalSuccessfulConnections returns the total number of connections that have been successfully handled.
func (m *ConnectionManager) GetTotalSuccessfulConnections() int64 {
	totalSuccessfulConnections := atomic.LoadInt64(&m.totalSuccessfulConnections)
	logrus.WithField("totalSuccessfulConnections", totalSuccessfulConnections).Debug("Retrieved total number of successful connections")
	return totalSuccessfulConnections
}

// connWithReader wraps a net.Conn to associate it with an io.Reader.
type connWithReader struct {
	net.Conn
	reader io.Reader
}

// Read reads data from the wrapped io.Reader.
func (c *connWithReader) Read(b []byte) (n int, err error) {
	n, err = c.reader.Read(b)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"localAddr":  c.Conn.LocalAddr().String(),
			"remoteAddr": c.Conn.RemoteAddr().String(),
		}).Error("Failed to read data from connection")
	}
	return n, err
}

// UpdateBytesSent updates the total bytes sent counter.
func (m *ConnectionManager) UpdateBytesSent(bytes int64) {
	atomic.AddInt64(&m.bytesSent, bytes)
}

// UpdateBytesReceived updates the total bytes received counter.
func (m *ConnectionManager) UpdateBytesReceived(bytes int64) {
	atomic.AddInt64(&m.bytesReceived, bytes)
}

// GetTotalBytesSent returns the total bytes sent.
func (m *ConnectionManager) GetTotalBytesSent() int64 {
	return atomic.LoadInt64(&m.bytesSent)
}

// GetTotalBytesReceived returns the total bytes received.
func (m *ConnectionManager) GetTotalBytesReceived() int64 {
	return atomic.LoadInt64(&m.bytesReceived)
}

// GetCurrentConnections returns the current number of active connections.
func (m *ConnectionManager) GetCurrentConnections() int {
	m.connMutex.Lock()
	defer m.connMutex.Unlock()
	return len(m.conns)
}

// GetLastRequestDuration returns the duration of the last request.
func (m *ConnectionManager) GetLastRequestDuration() time.Duration {
	// Return the last request duration.
	// Ensure you update this field appropriately in your connection handling logic.
	return m.lastRequestDuration
}

// SetupSocks5Server sets up a new SOCKS5 server with a custom Dial function.
// It returns the SOCKS5 server or an error if there was an issue setting it up.
func SetupSocks5Server() (*socks5.Server, error) {
	// Create an instance of SimpleDialer from the proxy package that prefers IPv6.
	dialer := &SimpleDialer{} // Use the proxy package qualifier

	// Create a socks5.Config and pass the SimpleDialer to it.
	conf := &socks5.Config{
		Dial: dialer.Dial, // Use the Dial method of SimpleDialer as the custom dial function.
	}

	// Create the SOCKS5 server with the configuration that includes your custom dialer.
	socksServer, err := socks5.New(conf)
	if err != nil {
		logrus.Error("Failed to create new SOCKS5 server: ", err)
		return nil, err
	}

	return socksServer, nil
}
