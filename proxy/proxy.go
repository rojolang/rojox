package proxy

import (
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
	"net"
	"sync"
	"time"
)

type ConnectionPool struct {
	mu        sync.Mutex
	conns     chan net.Conn
	maxSize   int
	waitGroup sync.WaitGroup
	closing   bool
	closed    bool
}

func NewConnectionPool(size int) *ConnectionPool {
	return &ConnectionPool{
		conns:   make(chan net.Conn, size),
		maxSize: size,
	}
}

func (p *ConnectionPool) Add(conn net.Conn) {
	p.mu.Lock()
	logrus.Debug("Lock acquired in Add")
	defer p.mu.Unlock()
	p.conns <- conn
	p.waitGroup.Add(1)
}

func (p *ConnectionPool) Get(ctx context.Context) (net.Conn, error) {
	p.mu.Lock()
	logrus.Debug("Lock acquired in Get")
	if p.closing {
		p.mu.Unlock()
		return nil, fmt.Errorf("connection pool closing")
	}
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("connection pool closed")
	}
	p.mu.Unlock()

	select {
	case conn := <-p.conns:
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *ConnectionPool) GetTotalConnections() int {
	p.mu.Lock()
	logrus.Debug("Lock acquired in GetTotalConnections")
	defer p.mu.Unlock()
	if p == nil {
		return 0
	}
	return len(p.conns)
}

func (p *ConnectionPool) Close() {
	p.mu.Lock()
	logrus.Debug("Lock acquired in Close")
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

func (p *ConnectionPool) AutoScale() {
	for {
		p.mu.Lock()
		logrus.Debug("Lock acquired in AutoScale")
		size := len(p.conns)
		p.mu.Unlock()

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
