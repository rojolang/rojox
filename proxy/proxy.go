package proxy

import (
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
	"net"
	"sync/atomic"
)

type ConnectionManager struct {
	totalRequests int64 // Total number of requests made.
	totalFailed   int64 // Total number of failed connection attempts.
}

// NewConnectionManager creates a new connection manager.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{}
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

// Close closes a connection.
func (m *ConnectionManager) Close(conn net.Conn) {
	if err := conn.Close(); err != nil {
		logrus.WithField("address", conn.RemoteAddr().String()).Errorf("Failed to close connection: %v", err)
	} else {
		logrus.WithField("address", conn.RemoteAddr().String()).Info("Successfully closed connection")
	}
}
