package conflict

import (
	"GossamerDB/internal/config"
	"sort"
)

type ConflictResolver interface {
	Resolve(versions []VersionedValue) []VersionedValue
	Name() string
}

func InitResolver(max int) ConflictResolver {
	switch config.ConfigObj.VectorClock.ConflictResolution {
	case config.VectorClockConflictResolutionLastWriteWins:
		return NewLwwResolver(max)
	case config.VectorClockConflictResolutionCustom:
		return NewMergeResolver(max)
	}
	return NewLwwResolver(max)
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
