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
)

// PrintStats prints stats every 5 seconds
func PrintStats(manager *proxy.ConnectionManager) {
	logrus.Info("Starting PrintStats goroutine")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		serverStats := logrus.Fields{}

		// Get and log server stats
		go func() {
			serverStats["public_ip"], _ = getAndLogStat("public IP", utils.GetPublicIP)
			serverStats["ip_type"] = utils.CheckIPType(serverStats["public_ip"].(string))
			serverStats["ipv6_ip"], _ = getAndLogStat("IPv6", utils.GetIPv6)

			// Update our metrics
			totalRequests.Add(float64(manager.GetTotalRequests()))
			totalFailed.Add(float64(manager.GetTotalFailed()))
			totalConnections.Add(float64(manager.GetTotalConnections()))

			uptime.Set(float64(manager.GetUptime().Seconds()))
			maxConcurrentConnections.Set(float64(manager.GetMaxConcurrentConnections()))
			totalFailedConnections.Add(float64(manager.GetTotalFailedConnections()))
			totalSuccessfulConnections.Add(float64(manager.GetTotalSuccessfulConnections()))

			cpuUsage, _ := utils.GetCurrentCPUUsage()
			currentCPUUsage.Set(cpuUsage)

			memUsage, _ := utils.GetCurrentMemoryUsage()
			currentMemoryUsage.Set(memUsage)

			logrus.WithFields(serverStats).Info("Server stats")
		}()
	}
}

// getAndLogStat gets a server stat using the provided function
func getAndLogStat(statName string, statFunc func() (string, error)) (string, error) {
	stat, err := statFunc()
	if err != nil {
		logrus.WithField(statName, err).Error("Unable to fetch stat")
		return "", err
	}
	logrus.WithField(statName, stat).Info("Fetched stat")
	return stat, nil
}

// getAndLogStatFloat gets a server stat using the provided function
func getAndLogStatFloat(statName string, statFunc func() (float64, error)) (float64, error) {
	stat, err := statFunc()
	if err != nil {
		logrus.WithField(statName, err).Error("Unable to fetch stat")
		return 0, err
	}
	logrus.WithField(statName, stat).Info("Fetched stat")
	return stat, nil
}
