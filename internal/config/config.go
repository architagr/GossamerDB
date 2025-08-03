// Package config provides the configuration structure for the application.
package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
)

// Config holds the configuration settings for the application.
type Config struct {
	// Cluster represents the configuration settings for the database cluster,
	// including details such as node addresses, replication settings, and other
	// cluster-specific parameters.
	Cluster ClusterInfo `json:"cluster" yaml:"cluster"`
	// Gossip contains the configuration settings for the gossip protocol,
	// which is used for node discovery and communication within the cluster.
	Gossip GossipInfo `json:"gossip" yaml:"gossip"`
	// MerkleTree contains the configuration settings for the Merkle tree,
	// which is used for data integrity and consistency checks across nodes.
	MerkleTree MerkleTreeInfo `json:"merkleTree" yaml:"merkleTree"`
	// VectorClock contains the configuration settings for vector clocks,
	// which are used for conflict resolution and versioning in distributed systems.
	VectorClock VectorClockInfo `json:"vectorClock" yaml:"vectorClock"`
	// Security contains the security settings for the application,
	// including mTLS configuration for secure communication between nodes.
	Security SecurityInfo `json:"security" yaml:"security"`
	// Persistence contains the configuration settings for data persistence,
	// including whether persistence is enabled, the backend used, and the storage path.
	Persistence PersistenceInfo `json:"persistence" yaml:"persistence"`
	// Monitoring contains the configuration settings for monitoring the application,
	// including whether monitoring is enabled and the minimum log level for monitoring.
	Monitoring MonitoringInfo `json:"monitoring" yaml:"monitoring"`
	// Repair contains the configuration settings for repair operations, including anti-entropy intervals.
	Repair RepairInfo `json:"repair" yaml:"repair"`
}

var (
	ConfigObj *Config
	once      sync.Once // Ensures that the configuration is loaded only once
)

func Load(filePath string) error {
	// Load the configuration from the specified file path.
	// This function should handle reading the file, parsing the contents,
	// and returning a Config instance or an error if loading fails.
	var err error
	once.Do(func() {
		var c *Config
		if filePath == "" {
			// If no file path is provided, initialize ConfigObj with default values.
			c = initializeDefaultConfig()
		} else if strings.HasSuffix(filePath, ".yaml") || strings.HasSuffix(filePath, ".yml") {
			// If the file is a YAML file, load the configuration from it.
			c, err = loadYamlConfig(filePath)
		} else if strings.HasSuffix(filePath, ".json") {
			// If the file is a JSON file, load the configuration from it.
			c, err = loadJsonConfig(filePath)
		} else {
			// If the file type is unsupported, return an error.
			err = fmt.Errorf("unsupported file type: %s", filePath)
		}
		if err == nil {
			if vErr := c.Validate(); vErr != nil {
				err = fmt.Errorf("invalid configuration: %w", vErr)
				return
			}
			ConfigObj = c
		}
	})
	return err
}

func initializeDefaultConfig() *Config {
	// This function initializes the ConfigObj with default values.
	return &Config{
		Cluster: ClusterInfo{
			Mode:              ClusterModeK8s,
			MaxNodesPerRegion: 10,
			TotalReplicas:     3,
			ReadQuorum:        2,
			WriteQuorum:       2,
			CoordinatorPort:   8080,
		},
		Gossip: GossipInfo{
			InitiationStrategy: GossipStrategyRumorMongering,
			SpreadStrategy:     GossipSpreadStrategyPush,
			Fanout:             3,
			IntervalMs:         1000,
			BufferSizePerMsg:   1024, // Buffer size for each gossip message
		},
		MerkleTree: MerkleTreeInfo{
			BucketSize: 100,
		},
		VectorClock: VectorClockInfo{
			ConflictResolution: VectorClockConflictResolutionLastWriteWins,
			MaxVersionsPerKey:  10,
		},
		Security: SecurityInfo{
			MTLS: MTLSInfo{
				Enabled:  false,
				CertFile: "",
				KeyFile:  "",
				CACert:   "",
			},
		},
		Persistence: PersistenceInfo{
			Enabled: false,
			Backend: "file",
		},
		Monitoring: MonitoringInfo{
			Enabled:     false,
			MinLogLevel: "debug",
		},
		Repair: RepairInfo{
			Enabled:                      true,
			AntiEntropyIntervalInSeconds: 1800,
		},
	}
}

// Validation for all nested fields
func (c *Config) Validate() error {
	if err := c.Cluster.Mode.Validate(); err != nil {
		return fmt.Errorf("cluster.mode: %w", err)
	}
	if err := c.Gossip.InitiationStrategy.Validate(); err != nil {
		return fmt.Errorf("gossip.initiationStrategy: %w", err)
	}
	if err := c.Gossip.SpreadStrategy.Validate(); err != nil {
		return fmt.Errorf("gossip.spreadStrategy: %w", err)
	}
	if err := c.VectorClock.ConflictResolution.Validate(); err != nil {
		return fmt.Errorf("vectorClock.conflictResolution: %w", err)
	}

	return nil
}

func loadYamlConfig(filePath string) (*Config, error) {
	// This function should implement the logic to read a YAML configuration file
	// and populate the ConfigObj with the parsed values.
	// It should return an error if the file cannot be read or parsed.
	log.Println("trying to read YAML config: ", filePath)
	yamlFile, err := os.ReadFile(filePath)
	if err != nil || len(yamlFile) == 0 {
		return nil, fmt.Errorf("yamlFile.Get err: %w ", err)
	}
	var c Config
	err = yaml.Unmarshal(yamlFile, &c)
	if err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}
	return &c, nil
}

func loadJsonConfig(filePath string) (*Config, error) {
	// This function should implement the logic to read a JSON configuration file
	// and populate the ConfigObj with the parsed values.
	// It should return an error if the file cannot be read or parsed.
	log.Println("trying to read JSON config: ", filePath)
	jsonFile, err := os.ReadFile(filePath)
	if err != nil || len(jsonFile) == 0 {
		return nil, fmt.Errorf("jsonFile.Get err: %w ", err)
	}
	var c Config
	err = json.Unmarshal(jsonFile, &c)
	if err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}
	return &c, nil
}
