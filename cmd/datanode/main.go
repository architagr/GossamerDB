package main

import (
	"GossamerDB/internal/config"
	"GossamerDB/internal/gossip"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
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

	// This is the entry point for the data node service.
	// The main function will initialize and start the data node.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startDataNode()
	runGossip(ctx)
}

func runGossip(ctx context.Context) {
	gossipEngine, err := gossip.NewEngine(config.ConfigObj.Gossip)
	if err != nil {
		log.Fatalf("failed to initialize gossip engine: %v", err)
	}

	go gossipEngine.Start(ctx)

	// Start gossip HTTP server
	gossipServer := gossip.NewServer(":"+config.ConfigObj.Gossip.Port, gossipEngine)
	go func() {
		if err := gossipServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("gossip server error: %v", err)
		}
	}()

}
func startDataNode() {
	// Initialization and startup logic for the data node goes here.
	// This could include setting up connections, loading configurations, etc.
	fmt.Printf("Starting data node with config %+v\n", config.ConfigObj)
}
