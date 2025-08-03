package main

import (
	"GossamerDB/internal/config"
	"flag"
	"fmt"
)

var (
	configFile = flag.String("config-file-path", "~/code/GossamerDB/config.yaml", "path of the config file")
)

func init() {
	flag.Parse()
	// This function is called before the main function to initialize the configuration.
	// It loads the configuration from a file or initializes it with default values.
	err := config.Load(*configFile) // Replace with your actual config file path
	if err != nil {
		panic(err) // Handle error appropriately in production code
	}
}

func main() {

	// This is the entry point for the data node service.
	// The main function will initialize and start the data node.
	startDataNode()
}
func startDataNode() {
	// Initialization and startup logic for the data node goes here.
	// This could include setting up connections, loading configurations, etc.
	fmt.Printf("Starting data node with config %+v\n", config.ConfigObj)
}
