package gossip

import (
	"GossamerDB/internal/config"
	"GossamerDB/pkg/model"
	"log"
	"time"
)

type AggregationStrategy struct{}

func (a *AggregationStrategy) GenerateMessage(state model.NodeHealthInfo) model.GossipMessage {
	healthy := time.Now()
	for _, val := range state {
		if !val.IsZero() {
			healthy = val
			break
		}
	}
	summary := model.NodeHealthInfo{"healthy": healthy}
	log.Printf("[STRATEGY] Aggregation sending summary gossip: %v", summary)
	return model.GossipMessage{
		SenderID:   config.SelfID,
		Timestamp:  time.Now(),
		NodeHealth: summary,
	}
}

func (a *AggregationStrategy) Merge(local model.NodeHealthInfo, incoming model.GossipMessage) model.NodeHealthInfo {
	// Could implement some aggregation logic; here just return local state
	return local
}
