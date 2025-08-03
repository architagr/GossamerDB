package storage

import (
	"errors"
	"sync"

	"GossamerDB/internal/conflict"
)

var (
	ErrKeyNotFound = errors.New("key not found")
)

// Store is the interface for key-value storage backend.
type Store interface {
	// Get returns all versions for a key (multiple possible due to concurrent writes)
	Get(key string) ([]conflict.VersionedValue, error)

	// Set stores a versioned value for a key. Handles merging of existing versions.
	Set(key string, v conflict.VersionedValue) error

	// Delete removes key entirely
	Delete(key string) error

	// ListKeys returns all keys stored (useful for building Merkle trees, scans)
	ListKeys() []string
}

// memoryStore is a simple in-memory implementation of Store for prototyping.
type memoryStore struct {
	mu       sync.RWMutex
	store    map[string][]conflict.VersionedValue
	resolver conflict.ConflictResolver
	// maxVersionsPerKey limits number of versions to keep per key.
	maxVersionsPerKey int
}

// NewMemoryStore returns a new in-memory storage with max version limit.
func NewMemoryStore(maxVersionsPerKey int) Store {
	return &memoryStore{
		store: make(map[string][]conflict.VersionedValue),
		// You may want to inject/configure conflict resolver here instead of hardcoding.
		resolver:          conflict.InitResolver(maxVersionsPerKey),
		maxVersionsPerKey: maxVersionsPerKey,
	}
}

func (m *memoryStore) Get(key string) ([]conflict.VersionedValue, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	versions, ok := m.store[key]
	if !ok || len(versions) == 0 {
		return nil, ErrKeyNotFound
	}
	return versions, nil
}

func (m *memoryStore) Set(key string, v conflict.VersionedValue) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing := m.store[key]
	updated := m.mergeVersions(existing, v)
	m.store[key] = updated
	return nil
}

func (m *memoryStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.store, key)
	return nil
}

func (m *memoryStore) ListKeys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys := make([]string, 0, len(m.store))
	for k := range m.store {
		keys = append(keys, k)
	}
	return keys
}

// mergeVersions merges a new versioned value into current versions,
// applies conflict resolution locally (e.g., merge resolver),
// and respects maxVersionsPerKey limit.
func (m *memoryStore) mergeVersions(existing []conflict.VersionedValue, newVersion conflict.VersionedValue) []conflict.VersionedValue {
	allVersions := append(existing, newVersion)
	resolved := m.resolver.Resolve(allVersions)
	return resolved
}
