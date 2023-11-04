package proxy

import (
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
	"net"
	"sync/atomic"
	"time"
)

type ConnectionManager struct {
	totalRequests              int64     // Total number of requests made.
	totalFailed                int64     // Total number of failed connection attempts.
	totalConnections           int64     // Total number of accepted connections.
	totalSuccessfulConnections int64     // Total number of successfully served connections.
	totalFailedConnections     int64     // Total number of connections that failed to be served.
	startTime                  time.Time // The time when the ConnectionManager was created.
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		startTime: time.Now(),
	}
}

// Connect creates a new connection.
func (m *ConnectionManager) Connect(ctx context.Context, network, address string) (net.Conn, error) {
	atomic.AddInt64(&m.totalRequests, 1)

	conn, err := net.Dial(network, address)
	if err != nil {
		atomic.AddInt64(&m.totalFailed, 1)
		logrus.WithFields(logrus.Fields{
			"network": network,
			"address": address,
		}).Errorf("Failed to create connection: %v", err)
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"network": network,
		"address": address,
	}).Info("Successfully created connection")

	return conn, nil
}

// GetTotalRequests returns the total number of requests made.
func (m *ConnectionManager) GetTotalRequests() int64 {
	return atomic.LoadInt64(&m.totalRequests)
}

// GetTotalFailed returns the total number of failed connection attempts.
func (m *ConnectionManager) GetTotalFailed() int64 {
	return atomic.LoadInt64(&m.totalFailed)
}

// AcceptConnection increments the totalConnections counter.
func (m *ConnectionManager) AcceptConnection() {
	atomic.AddInt64(&m.totalConnections, 1)
}

// GetTotalConnections returns the total number of accepted connections.
func (m *ConnectionManager) GetTotalConnections() int64 {
	return atomic.LoadInt64(&m.totalConnections)
}

// GetUptime returns the time duration since the ConnectionManager was created.
func (m *ConnectionManager) GetUptime() time.Duration {
	return time.Since(m.startTime)
}

// Close closes a connection.
func (m *ConnectionManager) Close(conn net.Conn) {
	if err := conn.Close(); err != nil {
		logrus.WithField("address", conn.RemoteAddr().String()).Errorf("Failed to close connection: %v", err)
	} else {
		logrus.WithField("address", conn.RemoteAddr().String()).Info("Successfully closed connection")
	}
}

// IncrementSuccessfulConnections increments the totalSuccessfulConnections counter.
func (m *ConnectionManager) IncrementSuccessfulConnections() {
	atomic.AddInt64(&m.totalSuccessfulConnections, 1)
}

// IncrementFailedConnections increments the totalFailedConnections counter.
func (m *ConnectionManager) IncrementFailedConnections() {
	atomic.AddInt64(&m.totalFailedConnections, 1)
}
