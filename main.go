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
	uxPtr := flag.Bool("ux", false, "Run the ux")

	flag.Parse()

	if *clientPtr {
		client.Run() // Replace with actual function to run client
	} else if *satellitePtr {
		satellite.Run() // Replace with actual function to run satellite
	} else if *uxPtr {
		ux.Run() // Replace with actual function to run server
	} else {
		fmt.Println("Please specify --client, --satellite, or --server")
		os.Exit(1)
	}
}
