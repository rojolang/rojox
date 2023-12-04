package stats

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rojolang/rojox/proxy"
	"github.com/rojolang/rojox/utils"
	"github.com/sirupsen/logrus"
	"time"
)

// Define our metrics
var (
	totalRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "server_total_requests",
		Help: "The total number of requests",
	})
	totalFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "server_total_failed",
		Help: "The total number of failed requests",
	})
	totalConnections = promauto.NewCounter(prometheus.CounterOpts{
		Name: "server_total_connections",
		Help: "The total number of connections",
	})
	uptime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "server_uptime",
		Help: "The uptime of the server",
	})
	maxConcurrentConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "server_max_concurrent_connections",
		Help: "The maximum number of concurrent connections",
	})
	totalFailedConnections = promauto.NewCounter(prometheus.CounterOpts{
		Name: "server_total_failed_connections",
		Help: "The total number of failed connections",
	})
	totalSuccessfulConnections = promauto.NewCounter(prometheus.CounterOpts{
		Name: "server_total_successful_connections",
		Help: "The total number of successful connections",
	})
	currentCPUUsage = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "server_current_cpu_usage",
		Help: "The current CPU usage",
	})
	currentMemoryUsage = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "server_current_memory_usage",
		Help: "The current memory usage",
	})
	activeConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "satellite_active_connections",
		Help: "Current number of active connections",
	})

	requestDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "satellite_request_duration_seconds",
		Help:    "Histogram of request handling durations",
		Buckets: prometheus.DefBuckets, // Use default buckets
	})

	bytesSent = promauto.NewCounter(prometheus.CounterOpts{
		Name: "satellite_bytes_sent_total",
		Help: "Total number of bytes sent by the satellite",
	})

	bytesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "satellite_bytes_received_total",
		Help: "Total number of bytes received by the satellite",
	})
)

// PrintStats prints stats every 5 seconds.
func PrintStats(manager *proxy.ConnectionManager) {
	logrus.Info("Starting PrintStats goroutine")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Update our metrics.
		totalRequests.Add(float64(manager.GetTotalRequests()))
		totalFailed.Add(float64(manager.GetTotalFailed()))
		totalConnections.Add(float64(manager.GetTotalConnections()))

		uptime.Set(float64(manager.GetUptime().Seconds()))
		maxConcurrentConnections.Set(float64(manager.GetMaxConcurrentConnections()))
		totalFailedConnections.Add(float64(manager.GetTotalFailedConnections()))
		totalSuccessfulConnections.Add(float64(manager.GetTotalSuccessfulConnections()))

		activeConnections.Set(float64(manager.GetCurrentConnections()))
		bytesSent.Add(float64(manager.GetTotalBytesSent()))
		bytesReceived.Add(float64(manager.GetTotalBytesReceived()))
		requestDuration.Observe(manager.GetLastRequestDuration().Seconds())

		// Fetch additional stats using utility functions from the utils package.
		publicIP, err := utils.GetPublicIP()
		if err != nil {
			logrus.WithError(err).Error("Unable to fetch public IP")
		}
		ipv6, err := utils.GetIPv6()
		if err != nil {
			logrus.WithError(err).Error("Unable to fetch IPv6 address")
		}
		cpuUsage, err := utils.GetCurrentCPUUsage()
		if err != nil {
			logrus.WithError(err).Error("Unable to fetch current CPU usage")
		}
		memoryUsage, err := utils.GetCurrentMemoryUsage()
		if err != nil {
			logrus.WithError(err).Error("Unable to fetch current memory usage")
		}

		// Update the Prometheus gauge metrics with the fetched values.
		currentCPUUsage.Set(cpuUsage)
		currentMemoryUsage.Set(memoryUsage)

		// Log the updated metrics.
		logrus.WithFields(logrus.Fields{
			"total_bytes_sent":             manager.GetTotalBytesSent(),
			"total_bytes_received":         manager.GetTotalBytesReceived(),
			"public_ip":                    publicIP,
			"ip_type":                      utils.CheckIPType(publicIP),
			"ipv6_ip":                      ipv6,
			"total_requests":               manager.GetTotalRequests(),
			"total_failed":                 manager.GetTotalFailed(),
			"total_connections":            manager.GetTotalConnections(),
			"uptime":                       manager.GetUptime().Seconds(),
			"max_concurrent_connections":   manager.GetMaxConcurrentConnections(),
			"total_failed_connections":     manager.GetTotalFailedConnections(),
			"total_successful_connections": manager.GetTotalSuccessfulConnections(),
			"current_cpu_usage":            cpuUsage,
			"current_memory_usage":         memoryUsage,
			"active_connections":           manager.GetCurrentConnections(),
			"request_duration_seconds":     manager.GetLastRequestDuration().Seconds(),
		}).Info("Server stats")
	}
}
