package main

import (
	"GossamerDB/internal/config"
	"GossamerDB/internal/security"
	"flag"
	"fmt"
)

var (
	configFile = flag.String("config-file-path", "~/code/GossamerDB/config.yaml", "path of the config file")
	nodeId     = flag.String("node-id", "", "id of the node")
)

func init() {
	flag.Parse()
	// This function is called before the main function to initialize the configuration.
	// It loads the configuration from a file or initializes it with default values.
	err := config.Load(*configFile) // Replace with your actual config file path
	if err != nil {
		panic(err) // Handle error appropriately in production code
	}

	config.InitSelfID(*nodeId)

}

func main() {
	// This is the entry point for the coordinator service.
	// The main function will initialize and start the coordinator.
	startCoordinator()
	_, err := security.LoadMTLSConfig()
	if err != nil {
		panic(err)
	}
}
func startCoordinator() {
	// Initialization and startup logic for the coordinator goes here.
	// This could include setting up connections, loading configurations, etc.
	fmt.Printf("ctarting coordinator node with config %+v\n", config.ConfigObj)
}
