package types

import "time"

// FeishuDocumentReparse is a durable, server-created request, not a publication grant.
// Its connector binding and process configuration never come from task headers.
type FeishuDocumentReparse struct {
	ID                  string                     `json:"id"`
	ReuseCheckpointID   string                     `json:"reuse_checkpoint_request_id,omitempty"`
	KnowledgeID         string                     `json:"knowledge_id"`
	DataSourceID        string                     `json:"data_source_id"`
	KnowledgeBaseID     string                     `json:"knowledge_base_id"`
	TenantID            uint64                     `json:"tenant_id"`
	RootID              string                     `json:"root_id"`
	ExternalID          string                     `json:"external_id"`
	FileType            string                     `json:"file_type"`
	ConfigHash          string                     `json:"config_hash"`
	KBConfigHash        string                     `json:"kb_config_hash"`
	PreviousProcessHash string                     `json:"previous_process_hash"`
	Initiator           TaskInitiator              `json:"initiator"`
	ProcessConfig       *KnowledgeProcessOverrides `json:"process_config,omitempty"`
	RequestedAt         time.Time                  `json:"requested_at"`
	RetryAfter          time.Time                  `json:"retry_after,omitempty"`
	Attempts            int                        `json:"attempts"`
	ProgressWindows     int                        `json:"progress_windows,omitempty"`
	CompletedChunks     int                        `json:"completed_chunks,omitempty"`
	TotalChunks         int                        `json:"total_chunks,omitempty"`
	DeliveryAttempts    int                        `json:"delivery_attempts"`
	ExecutionID         string                     `json:"execution_id,omitempty"`
	Generation          int64                      `json:"execution_generation,omitempty"`
	Published           string                     `json:"published_attempt,omitempty"`
	ErrorCode           string                     `json:"error_code,omitempty"`
}

// FeishuDocumentReparseStatus is the content-free shape exposed to an authorized editor.
type FeishuDocumentReparseStatus struct {
	ID              string `json:"id"`
	KnowledgeID     string `json:"knowledge_id"`
	State           string `json:"state"`
	Attempts        int    `json:"attempts"`
	CompletedChunks int    `json:"completed_chunks,omitempty"`
	TotalChunks     int    `json:"total_chunks,omitempty"`
	ErrorCode       string `json:"error_code,omitempty"`
}
