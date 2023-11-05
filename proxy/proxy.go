// Package proxy provides functionality to manage and monitor network connections.
package proxy

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"github.com/armon/go-socks5"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type Dialer interface {
	Dial(ctx context.Context, network, address string) (net.Conn, error)
}

type SimpleDialer struct{}

func (d *SimpleDialer) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	return net.Dial(network, address)
}

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
}

func NewConnectionManager(dialer Dialer) *ConnectionManager {
	return &ConnectionManager{
		dialer:    dialer,
		startTime: time.Now(),
	}
}

// Connect creates a new connection.
func (m *ConnectionManager) Connect(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := m.dialer.Dial(ctx, network, address)
	if err != nil {
		atomic.AddInt64(&m.totalFailed, 1)
		logrus.WithFields(logrus.Fields{
			"network": network,
			"address": address,
		}).Errorf("Failed to create connection: %v", err)
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	atomic.AddInt64(&m.totalRequests, 1)
	atomic.AddInt64(&m.totalSuccessfulConnections, 1)
	logrus.WithFields(logrus.Fields{
		"network": network,
		"address": address,
	}).Info("Successfully created connection")

	return conn, nil
}

// isZeroTierIP checks if the given IP address belongs to the ZeroTier network or is the local IP.
func isZeroTierIP(ip string, conn net.Conn) bool {
	_, zeroTierNet, _ := net.ParseCIDR("10.243.0.0/16") // replace with the actual IP range of your ZeroTier network

	// Check if the remote address is a ZeroTier IP or the local IP
	if zeroTierNet.Contains(net.ParseIP(ip)) || ip == "10.0.127.101" {
		return true
	}

	// If not, check if the local address is a ZeroTier IP or the local IP
	localIP, _, _ := net.SplitHostPort(conn.LocalAddr().String())
	return zeroTierNet.Contains(net.ParseIP(localIP)) || localIP == "10.0.127.101"
}

func (m *ConnectionManager) HandleConnection(socksServer *socks5.Server, conn net.Conn) {
	defer m.Close(conn) // Ensure the connection is closed when the goroutine exits

	ip, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"event":       "split_host_port_error",
			"local_addr":  conn.LocalAddr().String(),
			"remote_addr": conn.RemoteAddr().String(),
		}).Error("Failed to split host port: ", err)
		return
	}

	if !isZeroTierIP(ip, conn) {
		// If it's not a ZeroTier IP, close the connection
		logrus.WithField("address", conn.RemoteAddr().String()).Info("Rejected connection from non-ZeroTier IP")
		return
	}

	logrus.WithFields(logrus.Fields{
		"event":       "zerotier_connection",
		"local_addr":  conn.LocalAddr().String(),
		"remote_addr": conn.RemoteAddr().String(),
		"zerotier_ip": ip,
	}).Info("Accepted connection from ZeroTier IP")

	// Create a buffered reader for the connection
	reader := bufio.NewReader(conn)

	// Check for gzip header
	header, err := reader.Peek(2)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"event":       "peek_header_error",
			"local_addr":  conn.LocalAddr().String(),
			"remote_addr": conn.RemoteAddr().String(),
		}).Error("Failed to peek header: ", err)
		return
	}

	// If the data is compressed, create a gzip reader
	if header[0] == 0x1f && header[1] == 0x8b {
		gzipReader, err := gzip.NewReader(reader)
		if err != nil {
			atomic.AddInt64(&m.totalFailedConnections, 1)
			logrus.WithFields(logrus.Fields{
				"event":       "gzip_reader_error",
				"local_addr":  conn.LocalAddr().String(),
				"remote_addr": conn.RemoteAddr().String(),
			}).Error("Failed to create gzip reader: ", err)
			return
		}
		defer gzipReader.Close()
		reader = bufio.NewReader(gzipReader)
	}

	// Create a new connection from the buffered reader
	conn = &connWithReader{conn, reader}

	// Serve the connection
	if err := socksServer.ServeConn(conn); err != nil {
		atomic.AddInt64(&m.totalFailedConnections, 1)
		logrus.WithFields(logrus.Fields{
			"event":       "serve_connection_error",
			"local_addr":  conn.LocalAddr().String(),
			"remote_addr": conn.RemoteAddr().String(),
		}).Error("Failed to serve connection: ", err)
	} else {
		atomic.AddInt64(&m.totalSuccessfulConnections, 1)
		atomic.AddInt64(&m.totalRequests, 1)
		logrus.WithFields(logrus.Fields{
			"event":       "serve_connection_success",
			"local_addr":  conn.LocalAddr().String(),
			"remote_addr": conn.RemoteAddr().String(),
		}).Info("Successfully served connection")
	}
}

// GetMaxConcurrentConnections returns the maximum number of concurrent connections.
func (m *ConnectionManager) GetMaxConcurrentConnections() int64 {
	maxConcurrentConnections := atomic.LoadInt64(&m.maxConcurrentConnections)
	logrus.WithField("maxConcurrentConnections", maxConcurrentConnections).Info("Max concurrent connections")
	return maxConcurrentConnections
}

// Connect creates a new connection.

// GetTotalRequests returns the total number of requests made.
func (m *ConnectionManager) GetTotalRequests() int64 {
	return atomic.LoadInt64(&m.totalRequests)
}

// GetTotalFailed returns the total number of failed connection attempts.
func (m *ConnectionManager) GetTotalFailed() int64 {
	return atomic.LoadInt64(&m.totalFailed)
}

// AcceptConnection increments the totalConnections and currentConnections counters.
// It also updates the maxConcurrentConnections counter if necessary.
func (m *ConnectionManager) AcceptConnection() {
	atomic.AddInt64(&m.totalConnections, 1)
	currentConnections := atomic.AddInt64(&m.currentConnections, 1)
	if currentConnections > atomic.LoadInt64(&m.maxConcurrentConnections) {
		atomic.StoreInt64(&m.maxConcurrentConnections, currentConnections)
	}
}

// GetTotalConnections returns the total number of accepted connections.
func (m *ConnectionManager) GetTotalConnections() int64 {
	return atomic.LoadInt64(&m.totalConnections)
}

// GetUptime returns the time duration since the ConnectionManager was created.
func (m *ConnectionManager) GetUptime() time.Duration {
	return time.Since(m.startTime)
}

func (m *ConnectionManager) Close(conn net.Conn) {
	m.connMutex.Lock()
	defer m.connMutex.Unlock()

	addr := conn.RemoteAddr().String()
	if _, ok := m.conns[addr]; ok {
		if err := conn.Close(); err != nil {
			logrus.WithField("address", addr).Error("Failed to close connection: ", err)
		} else {
			logrus.WithField("address", addr).Info("Successfully closed connection")
		}
		delete(m.conns, addr)
	}
}

// GetTotalFailedConnections returns the total number of failed connections.
func (m *ConnectionManager) GetTotalFailedConnections() int64 {
	return atomic.LoadInt64(&m.totalFailedConnections)
}

// GetTotalSuccessfulConnections returns the total number of successful connections.
func (m *ConnectionManager) GetTotalSuccessfulConnections() int64 {
	return atomic.LoadInt64(&m.totalSuccessfulConnections)
}

// connWithReader is a net.Conn that has its own reader.
type connWithReader struct {
	net.Conn
	reader io.Reader
}

func (c *connWithReader) Read(b []byte) (n int, err error) {
	return c.reader.Read(b)
}
