package model

import "time"

type GossipMessage struct {
	SenderID   string          `json:"senderID" yaml:"senderID"`     // Unique ID of the sending node
	Timestamp  time.Time       `json:"timestamp" yaml:"timestamp"`   // Time message was generated
	NodeHealth map[string]bool `json:"nodeHealth" yaml:"nodeHealth"` // Map of nodeID → healthy status
}
