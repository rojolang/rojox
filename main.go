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
	// serverPtr := flag.Bool("server", false, "Run the server")
	uxPtr := flag.Bool("ux", false, "Run the UX")

	flag.Parse()

	switch {
	case *clientPtr:
		client.Run() // Replace with actual function to run client
	case *satellitePtr:
		satellite.Run() // Replace with actual function to run satellite
	//case *serverPtr:
	//	server.Run() // Replace with actuaxl function to run server
	case *uxPtr:
		ux.Run() // Replace with actual function to run UX
	default:
		fmt.Println("Please specify --client, --satellite, --server, or --ux")
		os.Exit(1)
	}
}
