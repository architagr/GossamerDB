package merkle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
)

// MerkleNode represents a node (internal or leaf) in the Merkle tree.
type MerkleNode struct {
	Hash  string
	Left  *MerkleNode
	Right *MerkleNode
	// For a leaf, KeyRange is non-empty and covers bucket/partition keys.
	KeyRange []string
}

// Tree manages the tree for a partition or node.
type Tree struct {
	BucketSize int // Number of keys per leaf node
	Root       *MerkleNode
	mu         sync.RWMutex
}

// NewTree creates a MerkleTree with given bucket size.
func NewTree(bucketSize int) *Tree {
	return &Tree{
		BucketSize: bucketSize,
	}
}

// Build builds/rebuilds the Merkle tree given a key-value map.
// keys must be sorted before calling!
func (t *Tree) Build(sortedKeys []string, kvs map[string][]byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	leaves := t.buildLeaves(sortedKeys, kvs)
	t.Root = buildMerkle(leaves)
}

// buildLeaves returns a slice of MerkleNodes (leaves).
func (t *Tree) buildLeaves(keys []string, kvs map[string][]byte) []*MerkleNode {
	var leaves []*MerkleNode
	for i := 0; i < len(keys); i += t.BucketSize {
		end := min(i+t.BucketSize, len(keys))
		bucket := keys[i:end]
		data := ""
		for _, k := range bucket {
			data += k + ":" + hex.EncodeToString(kvs[k])
		}
		h := sha256.Sum256([]byte(data))
		leaves = append(leaves, &MerkleNode{
			Hash:     hex.EncodeToString(h[:]),
			KeyRange: bucket,
		})
	}
	return leaves
}

// buildMerkle recursively builds the full tree from leaves.
func buildMerkle(nodes []*MerkleNode) *MerkleNode {
	if len(nodes) == 0 {
		return nil
	}
	if len(nodes) == 1 {
		return nodes[0]
	}
	var parents []*MerkleNode
	for i := 0; i < len(nodes); i += 2 {
		if i+1 < len(nodes) {
			data := nodes[i].Hash + nodes[i+1].Hash
			h := sha256.Sum256([]byte(data))
			parents = append(parents, &MerkleNode{
				Hash:  hex.EncodeToString(h[:]),
				Left:  nodes[i],
				Right: nodes[i+1],
			})
		} else {
			parents = append(parents, nodes[i])
		}
	}
	return buildMerkle(parents)
}

// RootHash returns the hex hash for the tree root.
func (t *Tree) RootHash() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.Root == nil {
		return ""
	}
	return t.Root.Hash
}

// Diff returns the list of KeyRanges in leaves that differ between this and another MerkleTree.
// Both trees must have been built from same (sorted) set of keys and same bucket size.
func (t *Tree) Diff(other *Tree) ([][]string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.Root == nil || other.Root == nil {
		return nil, errors.New("cannot diff: tree(s) not built")
	}
	diffs := make([][]string, 0)
	diffHelper(t.Root, other.Root, &diffs)
	return diffs, nil
}

// diffHelper recursively compares two trees, collecting differing leaf key ranges.
func diffHelper(a, b *MerkleNode, diffs *[][]string) {
	if a == nil || b == nil {
		return
	}
	if a.Hash == b.Hash {
		return
	}
	// If both are leaves, collect their key range.
	if a.Left == nil && a.Right == nil && b.Left == nil && b.Right == nil {
		*diffs = append(*diffs, a.KeyRange)
		return
	}
	// Try to compare children; handle imbalanced trees gracefully.
	if a.Left != nil && b.Left != nil {
		diffHelper(a.Left, b.Left, diffs)
	}
	if a.Right != nil && b.Right != nil {
		diffHelper(a.Right, b.Right, diffs)
	}
}
