package gossip

import (
	"GossamerDB/internal/config"
	"GossamerDB/pkg/model"
	"log"
	"time"
)

type AggregationStrategy struct{}

func (a *AggregationStrategy) GenerateMessage(state map[string]bool) model.GossipMessage {
	healthy := false
	for _, ok := range state {
		if ok {
			healthy = true
			break
		}
	}
	summary := map[string]bool{"healthy": healthy}
	log.Printf("[STRATEGY] Aggregation sending summary gossip: %v", summary)
	return model.GossipMessage{
		SenderID:   config.SelfID,
		Timestamp:  time.Now(),
		NodeHealth: summary,
		// Config:     *config.CurrentConfig,
	}
}

func (a *AggregationStrategy) Merge(local map[string]bool, incoming model.GossipMessage) map[string]bool {
	// Could implement some aggregation logic; here just return local state
	return local
}
