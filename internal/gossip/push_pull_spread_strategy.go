package gossip

import (
	"GossamerDB/pkg/model"
	"sync"
)

type PushPullSpreadStrategy struct {
	pushStrategy *PushSpreadStrategy
	pullStrategy *PullSpreadStrategy
}

func NewPushPull() *PushPullSpreadStrategy {
	return &PushPullSpreadStrategy{
		pushStrategy: &PushSpreadStrategy{},
		pullStrategy: &PullSpreadStrategy{},
	}
}

func (p *PushPullSpreadStrategy) Spread(msg model.GossipMessage, peers []string) {
	// Run push and pull concurrently
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		p.pushStrategy.Spread(msg, peers)
		wg.Done()
	}()
	go func() {
		p.pullStrategy.Spread(msg, peers)
		wg.Done()
	}()
	wg.Wait()
}
