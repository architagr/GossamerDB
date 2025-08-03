package config

import "fmt"

type GossipStrategy string

const (
	// GossipStrategyAntiEntropy indicates a anti-entropy gossip strategy.
	GossipStrategyAntiEntropy GossipStrategy = "anti-entropy"
	// GossipStrategyRumorMongering indicates a rumor-mongering gossip strategy.
	GossipStrategyRumorMongering GossipStrategy = "rumor-mongering"
	// GossipStrategyAggregation indicates a aggregation gossip strategy.
	GossipStrategyAggregation GossipStrategy = "aggregation"
)

func (gs GossipStrategy) String() string {
	return string(gs)
}
func (gs *GossipStrategy) Validate() error {
	switch *gs {
	case GossipStrategyAntiEntropy, GossipStrategyRumorMongering, GossipStrategyAggregation:
		return nil
	default:
		return fmt.Errorf("invalid gossip strategy: %s", *gs)
	}
}

type GossipSpreadStrategy string

const (
	// GossipSpreadStrategyPull indicates a pull spread strategy.
	GossipSpreadStrategyPull GossipSpreadStrategy = "pull"
	// GossipSpreadStrategyPush indicates an push spread strategy.
	GossipSpreadStrategyPush GossipSpreadStrategy = "push"
	// GossipSpreadStrategyPullPush indicates a pull-push spread strategy.
	GossipSpreadStrategyPullPush GossipSpreadStrategy = "pull-push"
)

func (gss GossipSpreadStrategy) String() string {
	return string(gss)
}
func (gss *GossipSpreadStrategy) Validate() error {
	switch *gss {
	case GossipSpreadStrategyPull, GossipSpreadStrategyPush, GossipSpreadStrategyPullPush:
		return nil
	default:
		return fmt.Errorf("invalid gossip spread strategy: %s", *gss)
	}
}

type GossipInfo struct {
	InitiationStrategy GossipStrategy       `json:"initiationStrategy" yaml:"initiationStrategy"` // Strategy for initiating gossip communication
	SpreadStrategy     GossipSpreadStrategy `json:"spreadStrategy" yaml:"spreadStrategy"`         // Strategy for spreading gossip messages
	Fanout             int                  `json:"fanout" yaml:"fanout"`                         // Number of nodes to which gossip messages are sent
	IntervalMs         int                  `json:"intervalMs" yaml:"intervalMs"`                 // Interval for gossip message sending in milliseconds
	BufferSizePerMsg   int                  `json:"bufferSizePerMsg" yaml:"bufferSizePerMsg"`     // Buffer size for each gossip message
}
