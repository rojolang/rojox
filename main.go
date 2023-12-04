package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/rojolang/rojox/client"
	"github.com/rojolang/rojox/satellite"
	"github.com/rojolang/rojox/server"
	"github.com/rojolang/rojox/ux"
	"os"
	"strings"
	"time"
)

const (
	loadBalancerBufferSize = 100              // Size of the channel buffer for handling connections
	healthCheckInterval    = 30 * time.Second // Interval for health checks on satellites
)

func main() {
	clientPtr := flag.Bool("client", false, "Run the client")
	satellitePtr := flag.Bool("satellite", false, "Run the satellite")
	uxPtr := flag.Bool("ux", false, "Run the UX")

	flag.Parse()

	switch {
	case *clientPtr:
		client.Run() // Run client
	case *satellitePtr:
		satellite.Run() // Run satellite
	case *uxPtr:
		runUX() // Run UX
	default:
		runInteractiveMode()
	}
}

func runInteractiveMode() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Please specify the type of program to run:")
	fmt.Println("1. Client")
	fmt.Println("2. Satellite")
	fmt.Println("3. UX")

	input, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Failed to read input: %v\n", err)
		os.Exit(1)
	}

	input = strings.TrimSpace(input)

	switch input {
	case "1":
		client.Run() // Run client
	case "2":
		satellite.Run() // Run satellite
	case "3":
		runUX() // Run UX
	default:
		fmt.Println("Invalid option. Please enter 1, 2, or 3.")
		os.Exit(1)
	}
}

func runUX() {
	// Create a new LoadBalancer with the specified buffer size and health check interval
	loadBalancer := server.NewLoadBalancer(loadBalancerBufferSize, healthCheckInterval)
	ux.Run(loadBalancer) // Run UX with the new LoadBalancer
}
