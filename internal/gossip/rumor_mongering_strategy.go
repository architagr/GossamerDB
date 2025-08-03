package gossip

import (
	"GossamerDB/internal/config"
	"GossamerDB/pkg/model"
	"log"
	"math/rand"
	"time"
)

type RumorMongeringStrategy struct{}

func (r *RumorMongeringStrategy) GenerateMessage(state model.NodeHealthInfo) model.GossipMessage {
	partial := make(model.NodeHealthInfo)
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

func (r *RumorMongeringStrategy) Merge(local model.NodeHealthInfo, incoming model.GossipMessage) model.NodeHealthInfo {
	for k, v := range incoming.NodeHealth {
		local[k] = v
	}
	return local
}
