package server

import (
	"errors"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"sync"
)

type LoadBalancer struct {
	satellites []string
	index      int
	mu         sync.Mutex
}

func NewLoadBalancer() *LoadBalancer {
	return &LoadBalancer{}
}

func (lb *LoadBalancer) RegisterSatellite(ip string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.satellites = append(lb.satellites, ip)
	logrus.WithField("ip", ip).Info("Registered new satellite")
}

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

	return ip, nil
}

func (lb *LoadBalancer) HandleConnection(conn net.Conn) {
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

func copyData(dst net.Conn, src net.Conn) {
	defer dst.Close()
	defer src.Close()

	_, err := io.Copy(dst, src)
	if err != nil {
		logrus.WithField("error", err).Error("Failed to copy data between connections")
	}
}
