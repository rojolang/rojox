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
	"sync/atomic"
	"time"
)

// ConnectionManager is responsible for managing and monitoring network connections.
type ConnectionManager struct {
	totalRequests              int64     // Total number of requests made.
	totalFailed                int64     // Total number of failed connection attempts.
	totalConnections           int64     // Total number of accepted connections.
	totalSuccessfulConnections int64     // Total number of successfully served connections.
	totalFailedConnections     int64     // Total number of connections that failed to be served.
	maxConcurrentConnections   int64     // Maximum number of concurrent connections.
	currentConnections         int64     // Current number of open connections.
	startTime                  time.Time // The time when the ConnectionManager was created.
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		startTime: time.Now(),
	}
}

// GetMaxConcurrentConnections returns the maximum number of concurrent connections.
func (m *ConnectionManager) GetMaxConcurrentConnections() int64 {
	maxConcurrentConnections := atomic.LoadInt64(&m.maxConcurrentConnections)
	logrus.WithField("maxConcurrentConnections", maxConcurrentConnections).Info("Max concurrent connections")
	return maxConcurrentConnections
}

// Connect creates a new connection.
func (m *ConnectionManager) Connect(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := net.Dial(network, address)
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

// HandleConnection serves a connection with the given SOCKS5 server and tracks the number of successful and failed requests.
func (m *ConnectionManager) HandleConnection(socksServer *socks5.Server, conn net.Conn) {
	defer m.Close(conn) // Ensure the connection is closed when the goroutine exits

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

// Close closes a connection and decrements the currentConnections counter.
func (m *ConnectionManager) Close(conn net.Conn) {
	if err := conn.Close(); err != nil {
		logrus.WithField("address", conn.RemoteAddr().String()).Errorf("Failed to close connection: %v", err)
	} else {
		logrus.WithField("address", conn.RemoteAddr().String()).Info("Successfully closed connection")
		atomic.AddInt64(&m.currentConnections, -1)
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
