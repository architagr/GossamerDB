package gossip

import (
	"GossamerDB/internal/config"
	"GossamerDB/pkg/model"
	"log"
	"math/rand"
	"time"
)

type RumorMongeringStrategy struct{}

func (r *RumorMongeringStrategy) GenerateMessage(state map[string]bool) model.GossipMessage {
	partial := make(map[string]bool)
	for k, v := range state {
		if rand.Intn(2) == 0 {
			partial[k] = v
		}
	}
	log.Printf("[STRATEGY] Rumor-Mongering generating gossip of %d nodes", len(partial))
	return model.GossipMessage{
		SenderID:   config.SelfID,
		Timestamp:  time.Now(),
		NodeHealth: partial,
	}
}

func (r *RumorMongeringStrategy) Merge(local map[string]bool, incoming model.GossipMessage) map[string]bool {
	for k, v := range incoming.NodeHealth {
		local[k] = v
	}
	return local
}
