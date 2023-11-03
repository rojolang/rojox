package stats

import (
	"github.com/rojolang/rojox/proxy"
	"github.com/rojolang/rojox/utils"
	"github.com/sirupsen/logrus"
	"time"
)

// PrintStats prints stats every 5 seconds
func PrintStats(pool *proxy.ConnectionPool) {
	logrus.Info("Starting PrintStats goroutine")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		serverStats := logrus.Fields{}

		// Get and log server stats
		statsCh := make(chan struct{})
		go func() {
			defer close(statsCh)
			serverStats["public_ip"], _ = getAndLogStat("public IP", utils.GetPublicIP)
			serverStats["ip_type"] = utils.CheckIPType(serverStats["public_ip"].(string))
			serverStats["ipv6_ip"], _ = getAndLogStat("IPv6", utils.GetIPv6)
			serverStats["current_cpu_usage"], _ = getAndLogStatFloat("current CPU usage", utils.GetCurrentCPUUsage)
			serverStats["current_mem_usage"], _ = getAndLogStatFloat("current memory usage", utils.GetCurrentMemoryUsage)
		}()

		// Wait for server stats to be fetched
		<-statsCh

		// Get connection pool stats
		serverStats["total_connections"] = pool.GetTotalConnections()
		serverStats["total_requests"] = pool.GetTotalRequests()
		serverStats["idle_connections"] = pool.GetIdleConnections()

		logrus.WithFields(serverStats).Info("Server stats")
	}
}

// getAndLogStat gets a server stat using the provided function and logs an error if one occurs
func getAndLogStat(statName string, statFunc func() (string, error)) (string, error) {
	stat, err := statFunc()
	if err != nil {
		logrus.Error("Unable to get ", statName, ": ", err)
	}
	return stat, err
}

// getAndLogStatFloat gets a server stat using the provided function and logs an error if one occurs
func getAndLogStatFloat(statName string, statFunc func() (float64, error)) (float64, error) {
	stat, err := statFunc()
	if err != nil {
		logrus.Error("Unable to get ", statName, ": ", err)
	}
	return stat, err
}
