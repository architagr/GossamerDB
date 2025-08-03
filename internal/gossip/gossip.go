package gossip

import (
	"GossamerDB/internal/config"
	"context"
	"log"
	"maps"
	"math/rand"
	"sync"
	"time"
)

var ()

type Engine struct {
	cfg          config.GossipInfo
	initiation   GossipStrategy
	spread       SpreadStrategy
	nodeHealth   map[string]bool
	peers        []string
	nodeHealthMu sync.RWMutex
	configLock   sync.RWMutex
	stopCh       chan struct{}
	stoppedCh    chan struct{}
}

func NewEngine(cfg config.GossipInfo) (*Engine, error) {
	initiation := GetGossipStrategy(cfg.InitiationStrategy)
	spread := GetSpreadStrategy(cfg.SpreadStrategy)
	return &Engine{
		cfg:          cfg,
		initiation:   initiation,
		spread:       spread,
		nodeHealth:   make(map[string]bool),
		peers:        make([]string, 0),
		nodeHealthMu: sync.RWMutex{},
		configLock:   sync.RWMutex{},
		stopCh:       make(chan struct{}),
		stoppedCh:    make(chan struct{}),
	}, nil
}

func (e *Engine) Start(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(e.cfg.IntervalMs) * time.Millisecond)
	defer ticker.Stop()

	log.Println("[GOSSIP] Starting gossip engine")

	for {
		select {
		case <-ctx.Done():
			log.Println("[GOSSIP] Stopping gossip engine")
			close(e.stoppedCh)
			return
		case <-ticker.C:
			e.doGossip()
		}
	}
}

// doGossip composes gossip messages and spreads to peers
func (e *Engine) doGossip() {
	state := e.GetNodeHealth()

	msg := e.initiation.GenerateMessage(state)

	peers := e.GetRandomPeers()

	if len(peers) == 0 {
		log.Println("[GOSSIP] No peers available to gossip")
		return
	}
	log.Printf("[GOSSIP] Gossiping to %d peers", len(peers))
	e.spread.Spread(msg, peers)
}

// Stop waits for engine to stop
func (e *Engine) WaitStopped() {
	<-e.stoppedCh
}

func (e *Engine) GetNodeHealth() map[string]bool {
	e.nodeHealthMu.RLock()
	defer e.nodeHealthMu.RUnlock()

	copy := make(map[string]bool)
	i := 0
	for k, v := range e.nodeHealth {
		copy[k] = v
		i++
		if i >= e.cfg.NodeInfoPerMsg {
			break
		}
	}
	return copy
}

func (e *Engine) UpdateNodeHealth(newHealth map[string]bool) {
	e.nodeHealthMu.Lock()
	defer e.nodeHealthMu.Unlock()
	maps.Copy(e.nodeHealth, newHealth)
}

func (e *Engine) AddPeer(url string) {
	e.nodeHealthMu.Lock()
	defer e.nodeHealthMu.Unlock()
	e.peers = append(e.peers, url)
}

func (e *Engine) GetRandomPeers() []string {
	e.configLock.RLock()
	defer e.configLock.RUnlock()

	if len(e.peers) == 0 {
		return []string{}
	}

	selected := make([]string, 0, e.cfg.Fanout)
	perm := rand.Perm(len(e.peers))
	for i := 0; i < e.cfg.Fanout && i < len(e.peers); i++ {
		selected = append(selected, e.peers[perm[i]])
	}
	return selected
}
