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
	go lb.ScheduleHealthChecks(healthCheckInterval)
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
			lb.logger.Info("Starting health check", zap.String("satellite", sat.IP))

			// Send a GET request to the external service.
			resp, err := http.Get("https://api64.ipify.org/")
			if err != nil {
				lb.mu.Lock()
				sat.Healthy = false
				lb.mu.Unlock()
				lb.logger.Error("Health check failed: Unable to reach external service",
					zap.String("satellite", sat.IP), zap.Error(err))
				return
			}
			defer resp.Body.Close()

			// Read the response body to log it.
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				lb.mu.Lock()
				sat.Healthy = false
				lb.mu.Unlock()
				lb.logger.Error("Health check failed: Unable to read response body",
					zap.String("satellite", sat.IP), zap.Error(err))
				return
			}

			lb.logger.Info("Health check response from external service",
				zap.String("satellite", sat.IP), zap.ByteString("response", body))

			// Mark the satellite as healthy if we got a successful response.
			if resp.StatusCode == http.StatusOK {
				lb.mu.Lock()
				sat.Healthy = true
				sat.LastHealthCheck = time.Now()
				lb.mu.Unlock()
				lb.logger.Info("Health check passed", zap.String("satellite", sat.IP))
			} else {
				lb.mu.Lock()
				sat.Healthy = false
				lb.mu.Unlock()
				lb.logger.Error("Health check failed: External service returned non-OK status",
					zap.String("satellite", sat.IP), zap.Int("status", resp.StatusCode))
			}
		}(satellite)
	}
	wg.Wait()
}

// ScheduleHealthChecks schedules health checks to run at regular intervals.
func (lb *LoadBalancer) ScheduleHealthChecks(interval time.Duration) {
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
// handleSingleConnection handles a single connection from the buffered channel.
func (lb *LoadBalancer) handleSingleConnection(conn net.Conn) {
	defer conn.Close()

	satellite, err := lb.NextSatellite()
	if err != nil {
		lb.logger.Error("Failed to get next satellite", zap.Error(err))
		return
	}

	lb.logger.Info("Routing connection", zap.String("satelliteIP", satellite.IP))
	satelliteConn, err := net.DialTimeout("tcp", satellite.IP+":9050", 5*time.Second)
	if err != nil {
		lb.logger.Error("Failed to connect to satellite", zap.String("zeroTierIP", satellite.IP), zap.Error(err))
		return
	}
	defer satelliteConn.Close()

	lb.mu.Lock()
	satellite.ActiveConns++
	lb.mu.Unlock()

	defer func() {
		lb.mu.Lock()
		satellite.ActiveConns--
		lb.mu.Unlock()
	}()

	go copyData(satelliteConn, conn, lb.logger)
	go copyData(conn, satelliteConn, lb.logger)
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
