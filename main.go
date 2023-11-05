package main

import (
	"flag"
	"fmt"
	"github.com/rojolang/rojox/client"
	"github.com/rojolang/rojox/satellite"
	"github.com/rojolang/rojox/ux"
	"os"
)

func main() {
	clientPtr := flag.Bool("client", false, "Run the client")
	satellitePtr := flag.Bool("satellite", false, "Run the satellite")
	uxPtr := flag.Bool("ux", false, "Run the UX")

	flag.Parse()

	switch {
	case *clientPtr:
		client.Run() // Replace with actual function to run client
	case *satellitePtr:
		satellite.Run() // Replace with actual function to run satellite
	case *uxPtr:
		// Change directory to where Prometheus is located
		os.Chdir("/path/to/prometheus/directory") // Replace with actual path
		ux.Run()                                  // Run UX
	default:
		fmt.Println("Please specify --client, --satellite, or --ux")
		os.Exit(1)
	}
}
