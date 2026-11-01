package types

import "time"

// Durable scopes separate recovery intents, retained evidence and audit events.
const (
	PersonalExactRetryScope      = "personal_exact_retry_v1"
	PersonalExactArchiveScope    = "personal_exact_archive_v1"
	PersonalExactRetryAuditScope = "personal_exact_retry_audit_v1"
)

// PersonalExactRetry is a server-owned durable intent, not a read capability.
// The original dispatch remains immutable. Every delivery is checked against
// this intent and must obtain a fresh claim after complete source preflight.
type PersonalExactRetry struct {
	Version             int                    `json:"version"`
	SealedGeneration    int64                  `json:"sealed_generation"`
	Source              PersonalExactReceipt   `json:"source"`
	StageSHA256         string                 `json:"stage_sha256"`
	SourceReceiptID     int64                  `json:"source_receipt_id"`
	SourceReceiptSHA    string                 `json:"source_receipt_sha256"`
	NotAfter            time.Time              `json:"not_after"`
	OriginalOperationID int64                  `json:"original_operation_id"`
	OriginalDispatchSHA string                 `json:"original_dispatch_sha256"`
	Task                DocumentProcessPayload `json:"task"`
}

// PersonalExactRetryAccepted is the complete public response. It contains no
// source proof, private file locator, lease, nonce or provider credential.
type PersonalExactRetryAccepted struct {
	KnowledgeID string `json:"knowledge_id"`
	OperationID int64  `json:"recovery_operation_id"`
	State       string `json:"state"`
	Recovery    string `json:"recovery_mode"`
}
