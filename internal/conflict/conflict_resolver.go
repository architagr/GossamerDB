package conflict

import (
	"log"
	"sort"
)

type ConflictResolver interface {
	Resolve(versions []VersionedValue) []VersionedValue
	Name() string
}

func resolveConflicts(resolver ConflictResolver, versions []VersionedValue) []VersionedValue {
	resolved := resolver.Resolve(versions)
	log.Printf("Conflict resolved with strategy %s: %d versions remain", resolver.Name(), len(resolved))
	return resolved
}
func PruneVersions(versions []VersionedValue, max int) []VersionedValue {
	if max < 1 || len(versions) <= max {
		return versions
	}
	// Sort by vector clock (or timestamp if present)
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Clock.Less(versions[j].Clock)
	})
	// Keep only the most recent `max` items
	return versions[len(versions)-max:]
}
