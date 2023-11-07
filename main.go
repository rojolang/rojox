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
		if err := client.Run(); err != nil { // Assuming Run returns an error
			fmt.Printf("Failed to run client: %v\n", err)
			os.Exit(1)
		}
	case *satellitePtr:
		if err := satellite.Run(); err != nil { // Assuming Run returns an error
			fmt.Printf("Failed to run satellite: %v\n", err)
			os.Exit(1)
		}
	case *uxPtr:
		if err := os.Chdir("/ux"); err != nil { // os.Chdir returns an error
			fmt.Printf("Failed to change directory: %v\n", err)
			os.Exit(1)
		}
		if err := ux.Run(); err != nil { // Assuming Run returns an error
			fmt.Printf("Failed to run UX: %v\n", err)
			os.Exit(1)
		}
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
		if err := client.Run(); err != nil { // Assuming Run returns an error
			fmt.Printf("Failed to run client: %v\n", err)
			os.Exit(1)
		}
	case "2":
		if err := satellite.Run(); err != nil { // Assuming Run returns an error
			fmt.Printf("Failed to run satellite: %v\n", err)
			os.Exit(1)
		}
	case "3":
		if err := os.Chdir("/ux"); err != nil { // os.Chdir returns an error
			fmt.Printf("Failed to change directory: %v\n", err)
			os.Exit(1)
		}
		if err := ux.Run(); err != nil { // Assuming Run returns an error
			fmt.Printf("Failed to run UX: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Println("Invalid option. Please enter 1, 2, or 3.")
		os.Exit(1)
	}
}
