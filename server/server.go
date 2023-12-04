package server

import (
	"context"
	"errors"
	"go.uber.org/zap"
	"io"
	"net"
	"net/http"
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
	logger       *zap.Logger
}

// NewLoadBalancer creates a new LoadBalancer instance with a buffered channel.
func NewLoadBalancer(bufferSize int, healthCheckInterval time.Duration) *LoadBalancer {
	logger, _ := zap.NewProduction() // Replace with zap.NewDevelopment() for development
	lb := &LoadBalancer{
		connChan:     make(chan net.Conn, bufferSize),
		shutdownChan: make(chan struct{}),
		logger:       logger,
	}
	lb.logger.Info("Creating new LoadBalancer")
	go lb.handleConnections()
	go lb.scheduleHealthChecks(healthCheckInterval)
	return lb
}

func (lb *LoadBalancer) RegisterSatellite(zeroTierIP string) {
	if zeroTierIP == "" {
		lb.logger.Error("Cannot register satellite: IP is empty")
		return
	}

	lb.mu.Lock()
	defer lb.mu.Unlock()

	for _, satellite := range lb.satellites {
		if satellite.IP == zeroTierIP {
			lb.logger.Info("Satellite already registered", zap.String("zeroTierIP", zeroTierIP))
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
	lb.logger.Info("Registered new satellite", zap.String("zeroTierIP", zeroTierIP))
}

// performHealthChecks runs health checks on all satellites concurrently.
// performHealthChecks runs health checks on all satellites concurrently.
func (lb *LoadBalancer) performHealthChecks() {
	lb.mu.RLock()
	satellites := make([]*SatelliteStatus, len(lb.satellites))
	copy(satellites, lb.satellites) // Copy the slice to avoid locking during iteration
	lb.mu.RUnlock()

	var wg sync.WaitGroup
	for _, satellite := range satellites {
		wg.Add(1)
		go func(sat *SatelliteStatus) {
			defer wg.Done()
			// Replace with actual health check logic (e.g., TCP ping or endpoint check)
			resp, err := http.Get("https://api.ipify.org")
			lb.mu.Lock() // Lock when modifying the satellite's data
			defer lb.mu.Unlock()
			if err != nil || resp.StatusCode != http.StatusOK {
				sat.Healthy = false
				lb.logger.Error("Health check failed", zap.String("zeroTierIP", sat.IP), zap.Error(err))
				if resp != nil {
					resp.Body.Close()
				}
				return
			}
			resp.Body.Close()
			// If the response is successful, mark the satellite as healthy
			sat.Healthy = true
			sat.LastHealthCheck = time.Now()
			lb.logger.Info("Health check passed", zap.String("zeroTierIP", sat.IP))
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
			lb.logger.Info("Stopping scheduled health checks")
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
	lb.logger.Info("Stopped handling connections")
}

// HandleConnection enqueues an incoming connection to the buffered channel.
func (lb *LoadBalancer) HandleConnection(conn net.Conn) {
	lb.connChan <- conn
}

// handleSingleConnection handles a single connection from the buffered channel.
func (lb *LoadBalancer) handleSingleConnection(conn net.Conn) {
	defer func(conn net.Conn) {
		err := conn.Close()
		if err != nil {
			lb.logger.Error("Failed to close client connection", zap.Error(err))
		}
	}(conn)

	satellite, err := lb.NextSatellite()
	if err != nil {
		lb.logger.Error("Failed to get next satellite", zap.Error(err))
		return
	}

	satelliteConn, err := net.DialTimeout("tcp", satellite.IP+":9050", 5*time.Second)
	if err != nil {
		lb.logger.Error("Failed to connect to satellite", zap.String("zeroTierIP", satellite.IP), zap.Error(err))
		return
	}
	defer func() {
		if err := satelliteConn.Close(); err != nil {
			lb.logger.Error("Failed to close satellite connection", zap.String("zeroTierIP", satellite.IP), zap.Error(err))
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

	go copyData(conn, satelliteConn, lb.logger)
	go copyData(satelliteConn, conn, lb.logger)
}

// copyData copies data between two connections and logs errors if they occur.
func copyData(dst net.Conn, src net.Conn, logger *zap.Logger) {
	if _, err := io.Copy(dst, src); err != nil {
		logger.Error("Failed to copy data between connections", zap.Error(err))
	}
	if err := dst.Close(); err != nil {
		logger.Error("Failed to close destination connection", zap.Error(err))
	}
	if err := src.Close(); err != nil {
		logger.Error("Failed to close source connection", zap.Error(err))
	}
}
