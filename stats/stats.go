package stats

import (
	"github.com/sirupsen/logrus"
	"rojox/proxy"
	"rojox/utils"
	"time"
)

// PrintStats prints stats every 5 seconds
func PrintStats(pool *proxy.ConnectionPool) {
	for {
		time.Sleep(5 * time.Second)

		// Get public IP
		publicIP, err := utils.GetPublicIP()
		if err != nil {
			logrus.Error("Unable to get public IP: ", err)
			continue
		}

		// Determine IP type
		ipType := utils.CheckIPType(publicIP)

		// Get IPv6
		ipv6, err := utils.GetIPv6()
		if err != nil {
			logrus.Error("Unable to get IPv6: ", err)
		}

		// Get current CPU usage
		cpuUsage, err := utils.GetCurrentCPUUsage()
		if err != nil {
			logrus.Error("Unable to get current CPU usage: ", err)
		}

		// Get current memory usage
		memoryUsage, err := utils.GetCurrentMemoryUsage()
		if err != nil {
			logrus.Error("Unable to get current memory usage: ", err)
		}

		logrus.WithFields(logrus.Fields{
			"total_connections": pool.GetTotalConnections(),
			"public_ip":         publicIP,
			"ip_type":           ipType,
			"ipv6_ip":           ipv6,
			"current_cpu_usage": cpuUsage,
			"current_mem_usage": memoryUsage,
		}).Info("Server stats")
	}
}
