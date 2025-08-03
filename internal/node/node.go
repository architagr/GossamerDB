package node

import (
	"sort"
	"sync"

	"GossamerDB/internal/config"
	"GossamerDB/internal/conflict"
	"GossamerDB/internal/merkle"
	"GossamerDB/internal/quorum"
	"GossamerDB/internal/storage"
)

// DataNode represents a distributed storage node.
type DataNode struct {
	id          string
	store       storage.Store
	merkleTree  *merkle.Tree
	quorum      *quorum.Quorum
	vectorClock *conflict.VectorClock

	mu sync.RWMutex

	// Additional fields for membership, gossip, repair can be added here
}

// NewDataNode constructs a new node with specified config.
func NewDataNode() (*DataNode, error) {
	q := quorum.New()
	cfg := config.ConfigObj
	return &DataNode{
		id:          config.SelfID,
		store:       storage.NewMemoryStore(cfg.VectorClock.MaxVersionsPerKey),
		quorum:      q,
		merkleTree:  merkle.NewTree(cfg.MerkleTree.BucketSize),
		vectorClock: conflict.NewVectorClock(),
	}, nil
}

// Delete removes a key and updates Merkle tree
func (n *DataNode) Delete(key string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	err := n.store.Delete(key)
	if err != nil {
		return err
	}
	n.rebuildMerkleTree() // optamize this to reduce latency of delete
	return nil
}

// Put stores a value for a key, increments vector clock, triggers Merkle update.
func (n *DataNode) Put(key string, value []byte) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.vectorClock.Increment(n.id)

	vv := conflict.VersionedValue{
		Value: value,
		Clock: n.vectorClock.Copy(),
	}
	n.rebuildMerkleTree() // optamize this to reduce latency of put
	return n.store.Set(key, vv)
}

// Get returns versions for a key.
func (n *DataNode) Get(key string) ([]conflict.VersionedValue, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.store.Get(key)
}

// ListKeys returns all keys stored locally.
func (n *DataNode) ListKeys() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return n.store.ListKeys()
}

// --- Merkle integration ---

// Rebuild (or update) the Merkle tree from store (called after each mutation)
func (n *DataNode) rebuildMerkleTree() {
	keys := n.store.ListKeys()
	sort.Strings(keys)
	kvs := map[string][]byte{}
	for _, k := range keys {
		versions, _ := n.store.Get(k)
		if len(versions) > 0 {
			kvs[k] = versions[0].Value // If you keep multiple values, you can hash/concat all
		}
	}
	n.merkleTree.Build(keys, kvs)
}

// GetMerkleRoot returns the hex root hash for anti-entropy comparison.
func (n *DataNode) GetMerkleRoot() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.merkleTree.RootHash()
}

// DiffMerkle compares with another tree (from a peer), returns differing key ranges to repair.
func (n *DataNode) DiffMerkle(peerRoot *merkle.Tree) ([][]string, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.merkleTree.Diff(peerRoot)
}
