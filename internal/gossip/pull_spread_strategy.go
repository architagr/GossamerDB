package gossip

import (
	"GossamerDB/pkg/model"
	"io"
	"log"
	"net/http"
)

type PullSpreadStrategy struct{}

func (p *PullSpreadStrategy) Spread(_ model.GossipMessage, peers []string) {
	for _, peer := range peers {
		go func(url string) {
			resp, err := http.Get(url + "/health")
			if err != nil {
				log.Printf("[ERROR] Failed pulling gossip from %s: %v", url, err)
				return
			}
			body, _ := io.ReadAll(resp.Body)
			if err := resp.Body.Close(); err != nil {
				log.Printf("[WARN] Failed closing body from %s: %v", url, err)
			}
			log.Printf("[PULL] Gossip pulled from %s → %s", url, string(body))
		}(peer)
	}
}
