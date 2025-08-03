package gossip

import (
	"GossamerDB/internal/config"
	"GossamerDB/pkg/model"
)

type SpreadStrategy interface {
	Spread(msg model.GossipMessage, peers []string)
}

type GossipStrategy interface {
	GenerateMessage(state map[string]bool) model.GossipMessage
	Merge(local map[string]bool, incoming model.GossipMessage) map[string]bool
}

// --- Spread Strategies ---

func GetSpreadStrategy(name config.GossipSpreadStrategy) SpreadStrategy {
	switch name {
	case config.GossipSpreadStrategyPush:
		return &PushSpreadStrategy{}
	case config.GossipSpreadStrategyPull:
		return &PullSpreadStrategy{}
	case config.GossipSpreadStrategyPullPush:
		return NewPushPull()
	default:
		return &PushSpreadStrategy{}
	}
}

// --- Gossip Initiation Strategies ---

func GetGossipStrategy(name config.GossipStrategy) GossipStrategy {
	switch name {
	case config.GossipStrategyAntiEntropy:
		return &AntiEntropyStrategy{}
	case config.GossipStrategyRumorMongering:
		return &RumorMongeringStrategy{}
	case config.GossipStrategyAggregation:
		return &AggregationStrategy{}
	default:
		return &AntiEntropyStrategy{}
	}
}
