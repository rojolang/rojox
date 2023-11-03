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
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	return conn, nil
}

// GetTotalRequests returns the total number of requests made.
func (m *ConnectionManager) GetTotalRequests() int64 {
	return atomic.LoadInt64(&m.totalRequests)
}

// Close closes a connection.
func (m *ConnectionManager) Close(conn net.Conn) {
	if err := conn.Close(); err != nil {
		logrus.Error("Failed to close connection: ", err)
	}
}
