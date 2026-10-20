package types

const KnowledgeDispatchScope = "knowledge_dispatch"

// Commit errors are ambiguous; a subsequent not-found read does not prove
// that an in-flight COMMIT cannot become visible later. Only a failure before
// COMMIT was attempted permits candidate-file cleanup.
type KnowledgeDispatchAdmissionError struct {
	Cause         error
	CommitUnknown bool
}

func (e *KnowledgeDispatchAdmissionError) Error() string { return e.Cause.Error() }
func (e *KnowledgeDispatchAdmissionError) Unwrap() error { return e.Cause }

// KnowledgeDispatch is immutable after acceptance. Revision is the exact
// downloaded content hash; delivery state lives in TaskPendingOp.Op.
type KnowledgeDispatch struct {
	PayloadVersion    int                    `json:"payload_version"`
	SourceID          string                 `json:"source_id,omitempty"`
	ExternalID        string                 `json:"external_id,omitempty"`
	AttemptID         string                 `json:"attempt_id"`
	LastErrorCategory string                 `json:"last_error_category,omitempty"`
	Revision          string                 `json:"revision"`
	Task              DocumentProcessPayload `json:"task"`
}
