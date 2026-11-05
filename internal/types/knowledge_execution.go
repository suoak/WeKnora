package types

import "time"

// KnowledgeExecutionState is metadata for an already authorized Knowledge.
// It exposes neither queue payloads nor private source identity or credentials.
type KnowledgeExecutionState struct {
	State      string     `json:"state"`
	LeaseUntil *time.Time `json:"lease_until,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	RetryAfter *time.Time `json:"retry_after,omitempty"`
}
