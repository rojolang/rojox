package server

import (
	"errors"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"sync"
	"time"
)

type LoadBalancer struct {
	satellites []string
	index      int
	mu         sync.Mutex
}

// NewLoadBalancer creates a new LoadBalancer instance.
func NewLoadBalancer() *LoadBalancer {
	logrus.Info("Creating new LoadBalancer") // Added info print
	return &LoadBalancer{}
}

// RegisterSatellite registers a new satellite IP address.
func (lb *LoadBalancer) RegisterSatellite(ip string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.satellites = append(lb.satellites, ip)
	logrus.WithField("ip", ip).Info("Registered new satellite")
	time.Sleep(1 * time.Second) // Reduced delay to 1 second
}

// NextSatellite returns the next satellite IP address.
func (lb *LoadBalancer) NextSatellite() (string, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if len(lb.satellites) == 0 {
		err := errors.New("no satellites registered")
		logrus.Error(err)
		return "", err
	}

	ip := lb.satellites[lb.index]
	lb.index = (lb.index + 1) % len(lb.satellites)

	logrus.WithField("ip", ip).Info("Selected next satellite") // Added info print

	return ip, nil
}

// HandleConnection handles an incoming connection.
func (lb *LoadBalancer) HandleConnection(conn net.Conn) {
	logrus.WithField("remote_addr", conn.RemoteAddr().String()).Info("Handling connection...") // Added info print
	defer conn.Close()

	ip, err := lb.NextSatellite()
	if err != nil {
		logrus.WithField("error", err).Error("Failed to get next satellite")
		return
	}

	satelliteConn, err := net.Dial("tcp", ip+":1080")
	if err != nil {
		logrus.WithFields(logrus.Fields{"ip": ip, "port": "1080", "error": err}).Error("Failed to connect to satellite")
		return
	}
	defer satelliteConn.Close()

	logrus.WithFields(logrus.Fields{"ip": ip, "port": "1080"}).Info("Connected to satellite")

	// Copy data between the incoming connection and the satellite
	go copyData(conn, satelliteConn)
	go copyData(satelliteConn, conn)
}

// copyData copies data between two connections.
func copyData(dst net.Conn, src net.Conn) {
	logrus.WithFields(logrus.Fields{
		"dst": dst.RemoteAddr().String(),
		"src": src.RemoteAddr().String(),
	}).Info("Copying data...") // Added info print
	defer dst.Close()
	defer src.Close()

	_, err := io.Copy(dst, src)
	if err != nil {
		logrus.WithField("error", err).Error("Failed to copy data between connections")
	}
}
