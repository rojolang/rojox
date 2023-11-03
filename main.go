package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/armon/go-socks5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var (
	activeConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "active_connections",
		Help: "Number of active connections",
	})
	idleConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "idle_connections",
		Help: "Number of idle connections",
	})
	logEntries = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "log_entries_total",
		Help: "Total number of log entries",
	})
)

func init() {
	prometheus.MustRegister(activeConnections, idleConnections, logEntries)
	logrus.SetFormatter(&logrus.JSONFormatter{})
}

type ConnectionPool struct {
	mu        sync.Mutex
	conns     chan net.Conn
	maxSize   int
	waitGroup sync.WaitGroup
	closing   bool
	closed    bool
}

func NewConnectionPool(maxSize int) *ConnectionPool {
	logrus.Info("Creating a new connection pool with size: ", maxSize)
	return &ConnectionPool{
		conns:   make(chan net.Conn, maxSize),
		maxSize: maxSize,
	}
}

func (p *ConnectionPool) Get(ctx context.Context) (net.Conn, error) {
	p.mu.Lock()
	if p.closing {
		p.mu.Unlock()
		logrus.WithFields(logrus.Fields{
			"event": "connection_get_during_closing",
		}).Error("Get called during closing")
		logEntries.Inc()
		return nil, fmt.Errorf("connection pool closing")
	}
	if p.closed {
		p.mu.Unlock()
		logrus.WithFields(logrus.Fields{
			"event": "connection_get_after_closed",
		}).Error("Get called after closed")
		logEntries.Inc()
		return nil, fmt.Errorf("connection pool closed")
	}
	p.mu.Unlock()

	select {
	case conn := <-p.conns:
		idleConnections.Dec()
		activeConnections.Inc()
		p.waitGroup.Add(1)
		ipType := checkIPType(conn.RemoteAddr().String())
		logrus.WithFields(logrus.Fields{
			"event":  "connection_get",
			"size":   len(p.conns),
			"ipType": ipType,
		}).Info("Get connection from pool")
		logEntries.Inc()
		return conn, nil
	case <-ctx.Done():
		logrus.Info("Failed to get a connection from the pool: ", ctx.Err())
		return nil, ctx.Err()
	}
}

func (p *ConnectionPool) Put(conn net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		conn.Close()
		activeConnections.Dec()
		p.waitGroup.Done()
		logrus.WithFields(logrus.Fields{
			"event": "connection_put_after_closed",
		}).Error("Put called after closed")
		logEntries.Inc()
		return
	}

	if len(p.conns) < p.maxSize {
		p.conns <- conn
		idleConnections.Inc()
		activeConnections.Dec()
		p.waitGroup.Done()
		logrus.WithFields(logrus.Fields{
			"event": "connection_put",
			"size":  len(p.conns),
		}).Info("Put connection back into pool")
		logEntries.Inc()
	} else {
		conn.Close()
		activeConnections.Dec()
		p.waitGroup.Done()
	}
}

func (p *ConnectionPool) Close() {
	logrus.Info("Closing the connection pool")
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}

	p.closing = true
	p.mu.Unlock()

	p.waitGroup.Wait()

	p.mu.Lock()
	p.closed = true
	close(p.conns)
	for conn := range p.conns {
		conn.Close()
	}
	p.mu.Unlock()
}

func (p *ConnectionPool) AutoScale() {
	for {
		p.mu.Lock()
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

func checkIPType(ip string) string {
	host, _, err := net.SplitHostPort(ip)
	if err != nil {
		return "Unknown"
	}

	parsedIP := net.ParseIP(host)
	if parsedIP.To4() != nil {
		return "IPv4"
	} else if len(parsedIP) == net.IPv6len {
		return "IPv6"
	}
	return "Unknown"
}

type loggingListener struct {
	net.Listener
}

func (ll *loggingListener) Accept() (net.Conn, error) {
	conn, err := ll.Listener.Accept()
	if err != nil {
		return nil, err
	}

	logrus.WithFields(logrus.Fields{
		"event": "connection_accept",
		"addr":  conn.RemoteAddr().String(),
	}).Info("Accepted new connection")

	return conn, nil
}
func main() {
	// Create a SOCKS5 server
	conf := &socks5.Config{}
	socksServer, err := socks5.New(conf)
	if err != nil {
		logrus.Fatal(err)
		os.Exit(1)
	}

	// Create a connection pool
	pool := NewConnectionPool(10)
	go pool.AutoScale()

	// Get the IP address of the eth0 interface
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		logrus.Fatal(err)
		os.Exit(1)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		logrus.Fatal(err)
		os.Exit(1)
	}
	eth0IP, _, err := net.ParseCIDR(addrs[0].String())
	if err != nil {
		logrus.Fatal(err)
		os.Exit(1)
	}

	// Start a goroutine to listen for incoming connections
	go func() {
		address := fmt.Sprintf("%s:1080", eth0IP.String())
		logrus.Info("Listening for incoming connections on ", address)
		listener, err := net.Listen("tcp", address)
		if err != nil {
			logrus.Fatal(err)
		}
		if err := socksServer.Serve(&loggingListener{listener}); err != nil {
			logrus.Error(err)
		}
	}()

	// Expose metrics over HTTPS
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Welcome to the server!")
	})
	http.Handle("/metrics", promhttp.Handler())
	httpServer := &http.Server{
		Addr:      ":8081",
		TLSConfig: &tls.Config{},
	}

	// Create a channel to listen for termination signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	// Start time
	startTime := time.Now()

	// Start a goroutine to print stats every 10 seconds
	go func() {
		for {
			time.Sleep(10 * time.Second)

			// Get public IP
			publicIP, err := getPublicIP()
			if err != nil {
				logrus.Error("Unable to get public IP: ", err)
			}

			// Get IPv6
			ipv6, err := getIPv6()
			if err != nil {
				logrus.Error("Unable to get IPv6: ", err)
			}

			// Get current CPU usage
			cpuUsage, err := getCurrentCPUUsage()
			if err != nil {
				logrus.Error("Unable to get current CPU usage: ", err)
			}

			// Get current memory usage
			memoryUsage, err := getCurrentMemoryUsage()
			if err != nil {
				logrus.Error("Unable to get current memory usage: ", err)
			}

			// Collect metrics
			metrics := make(chan prometheus.Metric)
			go activeConnections.Collect(metrics)
			go idleConnections.Collect(metrics)
			totalConnections := 0.0
			for metric := range metrics {
				dtoMetric := &dto.Metric{}
				metric.Write(dtoMetric)
				totalConnections += dtoMetric.GetGauge().GetValue()
			}

			logrus.WithFields(logrus.Fields{
				"total_connections": totalConnections,
				"local_ip":          eth0IP.String(),
				"public_ip":         publicIP,
				"ipv6_ip":           ipv6,
				"time_alive":        time.Since(startTime),
				"current_cpu_usage": cpuUsage,
				"current_mem_usage": memoryUsage,
			}).Info("Server stats")
		}
	}()

	go func() {
		<-quit

		// Stop accepting new connections
		if err := httpServer.Shutdown(context.Background()); err != nil {
			logrus.Fatal(err)
		}

		// Close all existing connections
		pool.Close()

		os.Exit(0)
	}()

	if err := httpServer.ListenAndServeTLS("cert.pem", "key.pem"); err != nil {
		logrus.Fatal(err)
		os.Exit(1)
	}
}

func getCurrentCPUUsage() (float64, error) {
	cpuPercent, err := cpu.Percent(0, false)
	if err != nil {
		return 0, err
	}
	return cpuPercent[0], nil
}

func getCurrentMemoryUsage() (float64, error) {
	memStat, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}
	return memStat.UsedPercent, nil
}

func getPublicIP() (string, error) {
	resp, err := http.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func getIPv6() (string, error) {
	iface, err := net.InterfaceByName("eth0")
	if err != nil {
		return "", err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			return "", err
		}
		if ip.To4() == nil && len(ip) == net.IPv6len {
			// This is an IPv6 address
			return ip.String(), nil
		}
	}

	return "", fmt.Errorf("IPv6 address not found for eth0")
}
