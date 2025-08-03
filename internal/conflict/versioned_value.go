package conflict

type VersionedValue struct {
	Value []byte
	Clock VectorClock
}
