package types

// Dedicated durable scope in the existing receipt store, never a runnable
// queue operation. It must survive queue drains, task retries and rollback.
const PersonalExactReceiptScope = "personal_exact_receipt_v1"

type PersonalExactReceipt struct {
	Version            int    `json:"version"`
	TenantID           uint64 `json:"tenant_id"`
	KnowledgeBaseID    string `json:"knowledge_base_id"`
	KnowledgeID        string `json:"knowledge_id"`
	ImportID           string `json:"import_id"`
	AttemptID          string `json:"attempt_id"`
	CandidateVersionID string `json:"candidate_version_id"`
	ExpectedActive     string `json:"expected_active"`
	Generation         int64  `json:"generation"`
	GrantEpoch         string `json:"grant_epoch"`
	AppConfigVersion   string `json:"app_config_version"`
	ConnectionIdentity string `json:"connection_identity"`
	Revision           string `json:"revision"`
	ResourceID         string `json:"resource_id"`
	ResourceHandle     string `json:"resource_handle"`
	BindingID          string `json:"binding_id"`
	FileSHA256         string `json:"file_sha256"`
	ManifestSHA256     string `json:"manifest_sha256"`
	FileMD5            string `json:"file_md5"`
	ParserOutputSHA256 string `json:"parser_output_sha256,omitempty"`
	ChunksSHA256       string `json:"chunks_sha256,omitempty"`
	EmbeddingsSHA256   string `json:"embeddings_sha256,omitempty"`
	DerivedSHA256      string `json:"derived_sha256,omitempty"`
	KnowledgeDBSHA256  string `json:"knowledge_db_sha256,omitempty"`
	ChunksDBManifest   string `json:"chunks_db_manifest,omitempty"`
	VectorsDBManifest  string `json:"vectors_db_manifest,omitempty"`
	TextChunks         int    `json:"text_chunks"`
	VectorRows         int    `json:"vector_rows"`
}
