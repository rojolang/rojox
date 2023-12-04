package server

import (
	"context"
	"errors"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"sync"
	"time"
)

type SatelliteStatus struct {
	IP              string
	ActiveConns     int
	LastHealthCheck time.Time
	Healthy         bool
}

type LoadBalancer struct {
	satellites   []*SatelliteStatus
	mu           sync.RWMutex // Use RWMutex for read/write locking
	wg           sync.WaitGroup
	connChan     chan net.Conn // Buffered channel for handling connections
	shutdownChan chan struct{} // Channel to signal shutdown
}

// NewLoadBalancer creates a new LoadBalancer instance with a buffered channel.
func NewLoadBalancer(bufferSize int, healthCheckInterval time.Duration) *LoadBalancer {
	lb := &LoadBalancer{
		connChan:     make(chan net.Conn, bufferSize),
		shutdownChan: make(chan struct{}),
	}
	logrus.Info("Creating new LoadBalancer")
	go lb.handleConnections()
	go lb.scheduleHealthChecks(healthCheckInterval)
	return lb
}

func (lb *LoadBalancer) RegisterSatellite(zeroTierIP string) {
	if zeroTierIP == "" {
		logrus.Error("Cannot register satellite: IP is empty")
		return
	}

	lb.mu.Lock()
	defer lb.mu.Unlock()

	for _, satellite := range lb.satellites {
		if satellite.IP == zeroTierIP {
			logrus.WithField("zeroTierIP", zeroTierIP).Info("Satellite already registered")
			return
		}
	}

	newSatellite := &SatelliteStatus{
		IP:              zeroTierIP,
		ActiveConns:     0,
		LastHealthCheck: time.Now(),
		Healthy:         true,
	}
	lb.satellites = append(lb.satellites, newSatellite)
	logrus.WithField("zeroTierIP", zeroTierIP).Info("Registered new satellite")
}

// performHealthChecks runs health checks on all satellites concurrently.
func (lb *LoadBalancer) performHealthChecks() {
	lb.mu.RLock() // Use read lock because we're only reading the slice here
	satellites := make([]*SatelliteStatus, len(lb.satellites))
	copy(satellites, lb.satellites) // Copy the slice to avoid locking during iteration
	lb.mu.RUnlock()

	var wg sync.WaitGroup
	for _, satellite := range satellites {
		wg.Add(1)
		go func(sat *SatelliteStatus) {
			defer wg.Done()
			// Replace with actual health check logic (e.g., TCP ping or endpoint check)
			conn, err := net.DialTimeout("tcp", sat.IP+":9050", 5*time.Second)
			lb.mu.Lock() // Lock when modifying the satellite's data
			if err != nil {
				sat.Healthy = false
				logrus.WithFields(logrus.Fields{"zeroTierIP": sat.IP, "error": err}).Error("Health check failed")
			} else {
				sat.Healthy = true
				sat.LastHealthCheck = time.Now()
				logrus.WithField("zeroTierIP", sat.IP).Info("Health check passed")
				conn.Close()
			}
			lb.mu.Unlock()
		}(satellite)
	}
	wg.Wait()
}

// scheduleHealthChecks schedules health checks to run at regular intervals.
func (lb *LoadBalancer) scheduleHealthChecks(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lb.performHealthChecks()
		case <-lb.shutdownChan:
			logrus.Info("Stopping scheduled health checks")
			return
		}
	}
}

// Shutdown gracefully shuts down the LoadBalancer.
func (lb *LoadBalancer) Shutdown(ctx context.Context) error {
	close(lb.shutdownChan) // Signal health checks to stop
	close(lb.connChan)     // Close the connections channel

	done := make(chan struct{})
	go func() {
		lb.wg.Wait() // Wait for all connection handling to complete
		close(done)
	}()

	select {
	case <-done:
		return nil // Shutdown completed
	case <-ctx.Done():
		return ctx.Err() // Shutdown timed out
	}
}

// NextSatellite returns the satellite with the least active connections.
func (lb *LoadBalancer) NextSatellite() (*SatelliteStatus, error) {
	lb.mu.RLock() // Use read lock because we're only reading the data
	defer lb.mu.RUnlock()

	if len(lb.satellites) == 0 {
		return nil, errors.New("no satellites registered")
	}

	var leastConnSatellite *SatelliteStatus
	for _, satellite := range lb.satellites {
		if !satellite.Healthy {
			continue
		}
		if leastConnSatellite == nil || satellite.ActiveConns < leastConnSatellite.ActiveConns {
			leastConnSatellite = satellite
		}
	}

	if leastConnSatellite == nil {
		return nil, errors.New("no healthy satellites available")
	}

	return leastConnSatellite, nil
}

// handleConnections handles incoming connections from the buffered channel.
func (lb *LoadBalancer) handleConnections() {
	for conn := range lb.connChan {
		lb.wg.Add(1) // Increment the wait group counter
		go func(c net.Conn) {
			defer lb.wg.Done() // Decrement the wait group counter when done
			lb.handleSingleConnection(c)
		}(conn)
	}
	logrus.Info("Stopped handling connections")
}

// HandleConnection enqueues an incoming connection to the buffered channel.
func (lb *LoadBalancer) HandleConnection(conn net.Conn) {
	lb.connChan <- conn
}

// handleSingleConnection handles a single connection from the buffered channel.
func (lb *LoadBalancer) handleSingleConnection(conn net.Conn) {
	defer conn.Close()

	satellite, err := lb.NextSatellite()
	if err != nil {
		logrus.WithField("error", err).Error("Failed to get next satellite")
		return
	}

	satelliteConn, err := net.DialTimeout("tcp", satellite.IP+":9050", 5*time.Second)
	if err != nil {
		logrus.WithFields(logrus.Fields{"zeroTierIP": satellite.IP, "error": err}).Error("Failed to connect to satellite")
		return
	}
	defer func() {
		if err := satelliteConn.Close(); err != nil {
			logrus.WithFields(logrus.Fields{"zeroTierIP": satellite.IP, "error": err}).Error("Failed to close satellite connection")
		}
	}()

	lb.mu.Lock()
	satellite.ActiveConns++
	lb.mu.Unlock()

	defer func() {
		lb.mu.Lock()
		satellite.ActiveConns--
		lb.mu.Unlock()
	}()

	go copyData(conn, satelliteConn)
	go copyData(satelliteConn, conn)
}

// copyData copies data between two connections.
func copyData(dst net.Conn, src net.Conn) {
	if _, err := io.Copy(dst, src); err != nil {
		logrus.WithField("error", err).Error("Failed to copy data between connections")
	}
	if err := dst.Close(); err != nil {
		logrus.WithField("error", err).Error("Failed to close destination connection")
	}
	if err := src.Close(); err != nil {
		logrus.WithField("error", err).Error("Failed to close source connection")
	}
}
