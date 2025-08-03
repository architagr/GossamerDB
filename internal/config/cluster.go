package config

import "fmt"

type ClusterMode string

const (
	// ClusterModeLocal indicates a local cluster setup.
	ClusterModeLocal ClusterMode = "local"
	// ClusterModeK8s indicates a k8s cluster setup.
	ClusterModeK8s ClusterMode = "k8s"
	// ClusterModeAws indicates a aws cluster setup.
	ClusterModeAws ClusterMode = "aws"
)

func (cm ClusterMode) String() string {
	return string(cm)
}

func (cm *ClusterMode) validate() error {
	switch *cm {
	case ClusterModeLocal, ClusterModeK8s, ClusterModeAws:
		return nil
	default:
		return fmt.Errorf("invalid cluster mode: %s", *cm)
	}
}

type ClusterInfo struct {
	Mode              ClusterMode `json:"mode" yaml:"mode"`                           // Mode of the cluster (e.g., "distributed", "standalone")
	VirtualNode       int         `json:"virtualNode" yaml:"virtualNode"`             // number of virtual nodes
	MaxNodesPerRegion int         `json:"maxNodesPerRegion" yaml:"maxNodesPerRegion"` // Maximum number of nodes allowed per region
	TotalReplicas     int         `json:"totalReplicas" yaml:"totalReplicas"`         // Total number of replicas for data redundancy
	ReadQuorum        int         `json:"readQuorum" yaml:"readQuorum"`               // Number of nodes required to read data
	WriteQuorum       int         `json:"writeQuorum" yaml:"writeQuorum"`             // Number of nodes required to write data
	CoordinatorPort   int         `json:"coordinatorPort" yaml:"coordinatorPort"`     // Port for the coordinator service
}

func (c *ClusterInfo) validate() error {
	if err := c.Mode.validate(); err != nil {
		return err
	}
	if c.ReadQuorum < 1 || c.WriteQuorum < 1 || c.TotalReplicas < 1 {
		return fmt.Errorf("all quorums must be >= 1")
	}
	if c.ReadQuorum+c.WriteQuorum <= c.TotalReplicas {
		return fmt.Errorf("invalid: R + W must be > N for strong consistency (R:%d + W:%d <= N:%d)", c.ReadQuorum, c.WriteQuorum, c.TotalReplicas)
	}
	if c.WriteQuorum > c.TotalReplicas || c.ReadQuorum > c.TotalReplicas {
		return fmt.Errorf("quorum cannot exceed total replicas")
	}

	return nil
}
