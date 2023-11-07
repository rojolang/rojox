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
	logrus.Info("Creating new LoadBalancer")
	return &LoadBalancer{}
}

// RegisterSatellite registers a new satellite IP address.
func (lb *LoadBalancer) RegisterSatellite(zeroTierIP string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.satellites = append(lb.satellites, zeroTierIP)
	logrus.WithField("zeroTierIP", zeroTierIP).Info("Registered new satellite")
	logrus.WithField("satellites", lb.satellites).Info("Current satellites") // Print the current list of satellites
	time.Sleep(1 * time.Second)
}

// NextSatellite returns the next satellite IP address.
// HandleConnection handles an incoming connection.
func (lb *LoadBalancer) HandleConnection(conn net.Conn) {
	logrus.WithField("remote_addr", conn.RemoteAddr().String()).Info("Handling connection")
	defer conn.Close()

	go func() {
		zeroTierIP, err := lb.NextSatellite()
		if err != nil {
			logrus.WithField("error", err).Error("Failed to get next satellite")
			return
		}

		logrus.WithField("zeroTierIP", zeroTierIP).Info("Dialing satellite")
		satelliteConn, err := net.Dial("tcp", zeroTierIP+":1080")
		if err != nil {
			logrus.WithFields(logrus.Fields{"zeroTierIP": zeroTierIP, "port": "1080", "error": err}).Error("Failed to connect to satellite")
			return
		}
		defer satelliteConn.Close()

		logrus.WithFields(logrus.Fields{"zeroTierIP": zeroTierIP, "port": "1080"}).Info("Connected to satellite")

		// Copy data between the incoming connection and the satellite
		logrus.WithField("zeroTierIP", zeroTierIP).Info("Copying data to satellite")
		copyData(conn, satelliteConn)
		logrus.WithField("zeroTierIP", zeroTierIP).Info("Copying data from satellite")
		copyData(satelliteConn, conn)
	}()
}

// NextSatellite returns the next satellite IP address.
func (lb *LoadBalancer) NextSatellite() (string, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	// If there are no satellites registered, return an error
	if len(lb.satellites) == 0 {
		err := errors.New("no satellites registered")
		logrus.Error(err)
		return "", err
	}

	zeroTierIP := lb.satellites[lb.index]
	lb.index = (lb.index + 1) % len(lb.satellites)

	logrus.WithField("zeroTierIP", zeroTierIP).Info("Selected next satellite")
	logrus.WithField("satellites", lb.satellites).Info("Current satellites") // Print the current list of satellites

	return zeroTierIP, nil
}

// copyData copies data between two connections.
func copyData(dst net.Conn, src net.Conn) {
	logrus.WithFields(logrus.Fields{
		"dst": dst.RemoteAddr().String(),
		"src": src.RemoteAddr().String(),
	}).Info("Copying data")
	defer dst.Close()
	defer src.Close()

	_, err := io.Copy(dst, src)
	if err != nil {
		logrus.WithField("error", err).Error("Failed to copy data between connections")
	}
}
