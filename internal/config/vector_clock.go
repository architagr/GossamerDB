package config

import "fmt"

type VectorClockConflictResolution string

const (
	// VectorClockConflictResolutionLastWriteWins indicates that the last write wins in conflict resolution.
	VectorClockConflictResolutionLastWriteWins VectorClockConflictResolution = "last-write-wins"
	// VectorClockConflictResolutionCustom indicates a custom conflict resolution strategy.
	VectorClockConflictResolutionCustom VectorClockConflictResolution = "custom"
)

func (vccr VectorClockConflictResolution) String() string {
	return string(vccr)
}
func (vccr *VectorClockConflictResolution) Validate() error {
	switch *vccr {
	case VectorClockConflictResolutionLastWriteWins, VectorClockConflictResolutionCustom:
		return nil
	default:
		return fmt.Errorf("invalid vector clock conflict resolution: %s", *vccr)
	}
}

type VectorClockInfo struct {
	// ConflictResolution Conflict resolution strategy for vector clocks
	ConflictResolution VectorClockConflictResolution `json:"conflictResolution" yaml:"conflictResolution"`
	// MaxVersionsPerKey Maximum number of versions allowed per key
	MaxVersionsPerKey int `json:"maxVersionsPerKey" yaml:"maxVersionsPerKey"`
}
