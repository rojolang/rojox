package proxy

import (
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ConnectionPool represents a pool of network connections.
type ConnectionPool struct {
	mu              sync.RWMutex  // Protects the fields below.
	conns           chan net.Conn // Pool of network connections.
	maxSize         int           // Maximum size of the pool.
	waitGroup       sync.WaitGroup
	closing         bool  // Indicates if the pool is closing.
	closed          bool  // Indicates if the pool is closed.
	totalRequests   int32 // Total number of requests made.
	idleConnections int32 // Number of idle connections in the pool.
}

// NewConnectionPool creates a new connection pool with the given size.
func NewConnectionPool(size int) *ConnectionPool {
	return &ConnectionPool{
		conns:   make(chan net.Conn, size),
		maxSize: size,
	}
}

// Add adds a connection to the pool.
func (p *ConnectionPool) Add(conn net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conns <- conn
	p.waitGroup.Add(1)
	atomic.AddInt32(&p.idleConnections, 1)
}

// Get retrieves a connection from the pool.
func (p *ConnectionPool) Get(ctx context.Context) (net.Conn, error) {
	p.mu.Lock()
	if p.closing {
		p.mu.Unlock()
		return nil, fmt.Errorf("connection pool closing")
	}
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("connection pool closed")
	}
	atomic.AddInt32(&p.totalRequests, 1)
	atomic.AddInt32(&p.idleConnections, -1)
	p.mu.Unlock()

	select {
	case conn := <-p.conns:
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// GetTotalConnections returns the total number of connections in the pool.
func (p *ConnectionPool) GetTotalConnections() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.conns)
}

// GetTotalRequests returns the total number of requests made.
func (p *ConnectionPool) GetTotalRequests() int {
	return int(atomic.LoadInt32(&p.totalRequests))
}

// GetIdleConnections returns the number of idle connections in the pool.
func (p *ConnectionPool) GetIdleConnections() int {
	return int(atomic.LoadInt32(&p.idleConnections))
}

// Close closes all connections in the pool.
func (p *ConnectionPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closing = true
	close(p.conns)
	for conn := range p.conns {
		if err := conn.Close(); err != nil {
			logrus.Error("Failed to close connection: ", err)
		}
	}
	p.closed = true
}

// AutoScale automatically scales the size of the pool based on demand.
func (p *ConnectionPool) AutoScale() {
	for {
		p.mu.RLock()
		size := len(p.conns)
		p.mu.RUnlock()

		if size < p.maxSize/2 {
			p.mu.Lock()
			p.maxSize *= 2
			newConns := make(chan net.Conn, p.maxSize)
			for conn := range p.conns {
				newConns <- conn
			}
			p.conns = newConns
			p.mu.Unlock()
		} else if size > p.maxSize*3/4 {
			p.mu.Lock()
			p.waitGroup.Wait()
			p.maxSize /= 2
			newConns := make(chan net.Conn, p.maxSize)
			for i := 0; i < p.maxSize; i++ {
				newConns <- <-p.conns
			}
			p.conns = newConns
			p.mu.Unlock()
		}

		time.Sleep(time.Minute)
	}
}
