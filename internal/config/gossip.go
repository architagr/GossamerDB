package config

import (
	"errors"
	"fmt"
)

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
func (gs *GossipStrategy) validate() error {
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
func (gss *GossipSpreadStrategy) validate() error {
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
	NodeInfoPerMsg     int                  `json:"nodeInfoPerMsg" yaml:"nodeInfoPerMsg"`         // node info for each gossip message
	Port               string               `json:"port" yaml:"port"`                             // posr on which we will run the gossip protocol
}

func (c *GossipInfo) validate() error {
	if err := c.InitiationStrategy.validate(); err != nil {
		return err
	}
	if err := c.SpreadStrategy.validate(); err != nil {
		return err
	}
	if c.Fanout < 1 {
		return errors.New("fanout must be positive")
	}
	if c.IntervalMs < 100 {
		return errors.New("IntervalMs must be >= 100ms")
	}
	if c.NodeInfoPerMsg < 5 {
		return errors.New("NodeInfoPerMsg must be >= 5")
	}
	return nil
}
