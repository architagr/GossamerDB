package conflict

import "log"

type LWWResolver struct {
	maxVersions int
}

func NewLwwResolver(max int) *LWWResolver {
	return &LWWResolver{
		maxVersions: max,
	}
}

func (r *LWWResolver) Resolve(versions []VersionedValue) []VersionedValue {
	if len(versions) == 0 {
		return nil
	}
	latest := versions[0]
	for _, v := range versions[1:] {
		cmp := v.Clock.Compare(latest.Clock)
		switch cmp {
		case 1: // v happened after latest
			latest = v
		case 2: // concurrent, use tiebreaker: lex order on string repr for determinism
			if v.Clock.String() > latest.Clock.String() {
				latest = v
			}
		}
	}
	log.Printf("[LWWResolver] Resolved to 1 version from %d", len(versions))

	// Enforce maxVersions if > 1 allows but LWW naturally returns 1.
	if r.maxVersions > 0 && r.maxVersions < 1 {
		log.Printf("[LWWResolver] maxVersions <1 ignored for LWW, returning 1 version.")
	}
	return []VersionedValue{latest}
}

func (r *LWWResolver) Name() string {
	return "Last Write Wins"
}
