package gossip

import (
	"GossamerDB/internal/config"
	"GossamerDB/pkg/model"
	"log"
	"time"
)

type AntiEntropyStrategy struct{}

func (a *AntiEntropyStrategy) GenerateMessage(state map[string]bool) model.GossipMessage {
	log.Printf("[STRATEGY] Anti-Entropy generating state with %d nodes", len(state))
	return model.GossipMessage{
		SenderID:   config.SelfID,
		Timestamp:  time.Now(),
		NodeHealth: state,
	}
}

func (a *AntiEntropyStrategy) Merge(local map[string]bool, incoming model.GossipMessage) map[string]bool {
	return incoming.NodeHealth // In real system might be a merge but simple override appropriate here
}
