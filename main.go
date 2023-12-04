package main

import (
	"bufio"
	"flag"
	"fmt"
	"github.com/rojolang/rojox/client"
	"github.com/rojolang/rojox/satellite"
	"github.com/rojolang/rojox/ux"
	"os"
	"strings"
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
		err := os.Chdir("./ux") // os.Chdir returns an error
		if err != nil {
			fmt.Printf("Failed to change directory: %v\n", err)
			os.Exit(1)
		}
		ux.Run() // Run UX
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
		err := os.Chdir("./ux") // os.Chdir returns an error
		if err != nil {
			fmt.Printf("Failed to change directory: %v\n", err)
			os.Exit(1)
		}
		ux.Run() // Run UX
	default:
		fmt.Println("Invalid option. Please enter 1, 2, or 3.")
		os.Exit(1)
	}
}
