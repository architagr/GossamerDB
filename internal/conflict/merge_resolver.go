package conflict

type MergeResolver struct {
	maxVersions int
}

func NewMergeResolver(max int) *MergeResolver {
	return &MergeResolver{
		maxVersions: max,
	}
}

func (r *MergeResolver) Resolve(versions []VersionedValue) []VersionedValue {
	if len(versions) == 0 {
		return nil
	}

	// Keep all that are not happened-before any other (concurrent or newer)
	var results []VersionedValue
	for i, v := range versions {
		dominated := false
		for j, other := range versions {
			if i == j {
				continue
			}
			if v.Clock.Compare(other.Clock) == -1 {
				dominated = true
				break
			}
		}
		if !dominated {
			results = append(results, v)
		}
	}

	return PruneVersions(results, r.maxVersions)
}

func (r *MergeResolver) Name() string {
	return "Merge Conflict (show all concurrent)"
}
