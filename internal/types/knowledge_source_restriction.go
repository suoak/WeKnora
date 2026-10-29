package types

// KnowledgeSourceRestriction is a metadata-only admission verdict. It does
// not assert that a historical task ran, failed, or is still queued.
type KnowledgeSourceRestriction struct {
	Restricted                bool   `json:"restricted"`
	SourceState               string `json:"source_state"`
	ReasonCode                string `json:"reason_code,omitempty"`
	HistoricalProcessingState string `json:"historical_processing_state"`
}

// UnknownKnowledgeSourceRestriction reports no verified admission or execution result.
func UnknownKnowledgeSourceRestriction() KnowledgeSourceRestriction {
	return KnowledgeSourceRestriction{SourceState: "unverified", HistoricalProcessingState: "unverified"}
}

// SourceProcessingRestrictedError is issued only after a current, exact-scope
// metadata check has proved hold/revoked. A generic 403 or SkipRetry is not
// this signal, and must retain its existing retry/security semantics.
type SourceProcessingRestrictedError struct{ State string }

func (*SourceProcessingRestrictedError) Error() string { return "source_processing_restricted" }
func (*SourceProcessingRestrictedError) Unwrap() error { return ErrLegacySourceRestricted }
