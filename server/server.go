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
	wg         sync.WaitGroup
}

// NewLoadBalancer creates a new LoadBalancer instance.
func NewLoadBalancer() *LoadBalancer {
	lb := &LoadBalancer{}
	logrus.WithField("loadBalancer", lb).Info("Creating new LoadBalancer")
	return lb
}

// RegisterSatellite registers a new satellite IP address.
func (lb *LoadBalancer) RegisterSatellite(zeroTierIP string) {
	if zeroTierIP == "" {
		logrus.Error("Cannot register satellite: IP is empty")
		return
	}

	logrus.WithField("zeroTierIP", zeroTierIP).Info("Registering satellite") // Log the IP being registered
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.wg.Add(1) // Wait for the registration to complete
	lb.satellites = append(lb.satellites, zeroTierIP)
	lb.wg.Done() // Registration complete
	logrus.WithField("zeroTierIP", zeroTierIP).Info("Registered new satellite")
	logrus.WithField("satellites", lb.satellites).Info("Current satellites") // Print the current list of satellites
	logrus.WithField("loadBalancer", lb).Info("Current LoadBalancer")        // Print the address of the LoadBalancer instance
}

// NextSatellite returns the next satellite IP address.
func (lb *LoadBalancer) NextSatellite() (string, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	// If there are no satellites registered, return an error
	if len(lb.satellites) == 0 {
		err := errors.New("no satellites registered")
		logrus.WithField("satellites", lb.satellites).Info("Satellites at the time of error") // Print the current list of satellites at the time of error
		logrus.Error(err)
		return "", err
	}

	zeroTierIP := lb.satellites[lb.index]
	lb.index = (lb.index + 1) % len(lb.satellites)

	logrus.WithField("zeroTierIP", zeroTierIP).Info("Selected next satellite")
	logrus.WithField("satellites", lb.satellites).Info("Current satellites") // Print the current list of satellites
	logrus.WithField("loadBalancer", lb).Info("Current LoadBalancer")        // Print the address of the LoadBalancer instance

	return zeroTierIP, nil
}

// HandleConnection handles an incoming connection.
func (lb *LoadBalancer) HandleConnection(conn net.Conn) {
	logrus.WithField("remote_addr", conn.RemoteAddr().String()).Info("Handling connection")
	defer func() {
		logrus.WithField("remote_addr", conn.RemoteAddr().String()).Info("Closing connection")
		conn.Close()
	}()

	lb.wg.Wait() // Wait for the satellite registration to complete

	go func() {
		logrus.WithField("loadBalancer", lb).Info("Current LoadBalancer before NextSatellite")        // Print the address of the LoadBalancer instance
		logrus.WithField("satellites", lb.satellites).Info("Current satellites before NextSatellite") // Print the current list of satellites
		zeroTierIP, err := lb.NextSatellite()
		if err != nil {
			logrus.WithField("error", err).Error("Failed to get next satellite")
			return
		}

		logrus.WithField("zeroTierIP", zeroTierIP).Info("Dialing satellite")
		satelliteConn, err := net.DialTimeout("tcp", zeroTierIP+":1080", 5*time.Second)
		if err != nil {
			logrus.WithFields(logrus.Fields{"zeroTierIP": zeroTierIP, "port": "1080", "error": err}).Error("Failed to connect to satellite via ZeroTier IP")

			// Try connecting with the public IP
			publicIP := "" // Replace with the public IP of the satellite
			logrus.WithField("publicIP", publicIP).Info("Dialing satellite via public IP")
			satelliteConn, err = net.DialTimeout("tcp", publicIP+":1080", 5*time.Second)
			if err != nil {
				logrus.WithFields(logrus.Fields{"publicIP": publicIP, "port": "1080", "error": err}).Error("Failed to connect to satellite via public IP")
				return
			}
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
