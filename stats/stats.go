package stats

import (
	"fmt"
	"github.com/rojolang/rojox/proxy"
	"github.com/rojolang/rojox/utils"
	"github.com/sirupsen/logrus"
	"time"
)

// PrintStats prints stats every 5 seconds
func PrintStats(manager *proxy.ConnectionManager) error {
	logrus.Info("Starting PrintStats goroutine")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		serverStats := logrus.Fields{}

		// Get and log server stats
		statsCh := make(chan error)
		go func() {
			defer close(statsCh)
			var err error
			serverStats["public_ip"], err = getAndLogStat("public IP", utils.GetPublicIP)
			if err != nil {
				statsCh <- err
				return
			}
			serverStats["ip_type"] = utils.CheckIPType(serverStats["public_ip"].(string))
			serverStats["ipv6_ip"], err = getAndLogStat("IPv6", utils.GetIPv6)
			if err != nil {
				statsCh <- err
				return
			}
			serverStats["current_cpu_usage"], err = getAndLogStatFloat("current CPU usage", utils.GetCurrentCPUUsage)
			if err != nil {
				statsCh <- err
				return
			}
			serverStats["current_mem_usage"], err = getAndLogStatFloat("current memory usage", utils.GetCurrentMemoryUsage)
			if err != nil {
				statsCh <- err
			}
		}()

		// Wait for server stats to be fetched
		if err := <-statsCh; err != nil {
			return err
		}

		// Get connection manager stats
		serverStats["total_requests"] = manager.GetTotalRequests()

		logrus.WithFields(serverStats).Info("Server stats")
	}

	return nil
}

// getAndLogStat gets a server stat using the provided function
func getAndLogStat(statName string, statFunc func() (string, error)) (string, error) {
	stat, err := statFunc()
	if err != nil {
		return "", fmt.Errorf("unable to get %s: %w", statName, err)
	}
	logrus.WithField(statName, stat).Info("Fetched stat")
	return stat, nil
}

// getAndLogStatFloat gets a server stat using the provided function
func getAndLogStatFloat(statName string, statFunc func() (float64, error)) (float64, error) {
	stat, err := statFunc()
	if err != nil {
		return 0, fmt.Errorf("unable to get %s: %w", statName, err)
	}
	logrus.WithField(statName, stat).Info("Fetched stat")
	return stat, nil
}
