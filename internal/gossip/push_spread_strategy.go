package gossip

import (
	"GossamerDB/pkg/model"
	"bytes"
	"encoding/json"
	"log"
	"net/http"
)

type PushSpreadStrategy struct{}

func (p *PushSpreadStrategy) Spread(msg model.GossipMessage, peers []string) {
	for _, peer := range peers {
		go func(url string) {
			payload, err := json.Marshal(msg)
			if err != nil {
				log.Printf("[ERROR] Failed to marshal gossip message: %v", err)
				return
			}
			log.Printf("[SEND] Gossip → %s | Payload size: %d bytes", url, len(payload))

			resp, err := http.Post(url+"/gossip", "application/json", bytes.NewBuffer(payload))
			if err != nil {
				log.Printf("[ERROR] Failed sending gossip to %s: %v", url, err)
				return
			}
			resp.Body.Close()
			log.Printf("[ACK] Gossip sent to %s", url)
		}(peer)
	}
}
