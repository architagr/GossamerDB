package quorum

import "GossamerDB/internal/config"

// Quorum provides methods to check for required acks and to validate config.
type Quorum struct {
}

func New() *Quorum {
	return &Quorum{}
}
func (q *Quorum) RequiredReadAcks() int  { return config.ConfigObj.Cluster.ReadQuorum }
func (q *Quorum) RequiredWriteAcks() int { return config.ConfigObj.Cluster.WriteQuorum }
func (q *Quorum) TotalReplicas() int     { return config.ConfigObj.Cluster.TotalReplicas }

// IsReadQuorumMet returns true if enough read acks were received.
func (q *Quorum) IsReadQuorumMet(acks int) bool {
	return acks >= q.RequiredReadAcks()
}

// IsWriteQuorumMet returns true if enough write acks were received.
func (q *Quorum) IsWriteQuorumMet(acks int) bool {
	return acks >= q.RequiredWriteAcks()
}
