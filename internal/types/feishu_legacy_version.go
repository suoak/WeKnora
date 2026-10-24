package types

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// This identity is source-owned, never inferred from a title, file hash or URL.
type FeishuLegacyResource struct {
	ID                 string `gorm:"primaryKey"`
	TenantID           uint64
	KnowledgeBaseID    string
	DataSourceID       string
	ExternalID         string
	KnowledgeID        string
	Generation         int64
	State              string
	CurrentAttempt     string
	ActiveAttempt      string
	SourceRevision     string
	OriginalFilePath   string          `json:"-"`
	QuarantineSnapshot json.RawMessage `json:"-" gorm:"type:jsonb"`
	UpdatedAt          time.Time
}

func (FeishuLegacyResource) TableName() string { return "feishu_legacy_resources" }

type FeishuLegacyAttempt struct {
	ID           string `gorm:"primaryKey"`
	ResourceID   string
	Generation   int64
	OwnerNonce   string `json:"-"`
	LeaseUntil   time.Time
	Status       string
	Revision     string
	SourceHash   string
	ExpectedHash string
	Manifest     json.RawMessage `gorm:"type:jsonb"`
	Receipt      json.RawMessage `gorm:"type:jsonb"`
	CreatedAt    time.Time
}

func (FeishuLegacyAttempt) TableName() string { return "feishu_legacy_attempts" }

// Payloads live exclusively in private tables until a single PostgreSQL commit.
type FeishuLegacyManifest struct {
	SourceIdentity   *FeishuLegacySourceIdentity `json:"source_identity,omitempty"`
	Knowledge        Knowledge                   `json:"knowledge"`
	Chunks           []*Chunk                    `json:"chunks"`
	ChunkHeaders     map[string]string           `json:"chunk_headers"`
	EmbeddingModelID string                      `json:"embedding_model_id"`
	KBConfigSHA256   string                      `json:"kb_config_sha256"`
	ModelStamp       string                      `json:"model_stamp"`
	StorageBackendID string                      `json:"storage_backend_id,omitempty"`
	StorageStamp     string                      `json:"storage_stamp,omitempty"`
	Dimension        int                         `json:"dimension"`
	SHA256           string                      `json:"sha256"`
	Assets           []FeishuLegacyAsset         `json:"assets,omitempty"`
	VLMModelID       string                      `json:"vlm_model_id,omitempty"`
	VLMModelStamp    string                      `json:"vlm_model_stamp,omitempty"`
}

// FeishuLegacyAsset is an immutable file in its attempt's private directory. The
// database publication pointer, never the filesystem upload, admits reads.
type FeishuLegacyAsset struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Bytes    int64  `json:"bytes"`
	MimeType string `json:"mime_type"`
}

// FeishuLegacySourceIdentity binds a versioned canonical package digest to its source name and title.
type FeishuLegacySourceIdentity struct {
	Algorithm string `json:"algorithm"`
	Digest    string `json:"digest"`
}
type FeishuLegacyReceipt struct {
	Created        bool      `json:"created"`
	ResourceID     string    `json:"resource_id"`
	AttemptID      string    `json:"attempt_id"`
	Generation     int64     `json:"generation"`
	KnowledgeID    string    `json:"knowledge_id"`
	ManifestSHA256 string    `json:"manifest_sha256"`
	Chunks         int64     `json:"chunks"`
	ParentChunks   int64     `json:"parent_chunks,omitempty"`
	IndexedChunks  int64     `json:"indexed_chunks,omitempty"`
	Vectors        int64     `json:"vectors"`
	BM25Rows       int64     `json:"bm25_rows"`
	Dimension      int       `json:"dimension"`
	Assets         int       `json:"assets,omitempty"`
	PublishedAt    time.Time `json:"published_at"`
}

type LegacyReceiptSet struct {
	mu       sync.Mutex
	receipts map[string]FeishuLegacyReceipt
}
type (
	legacyReceiptContextKey        struct{}
	legacyReconciliationContextKey struct{}
)

func WithLegacyFullReconciliation(ctx context.Context) context.Context {
	return context.WithValue(ctx, legacyReconciliationContextKey{}, true)
}

func LegacyFullReconciliation(ctx context.Context) bool {
	confirmed, _ := ctx.Value(legacyReconciliationContextKey{}).(bool)
	return confirmed
}

func WithLegacyReceiptSet(ctx context.Context) context.Context {
	return context.WithValue(ctx, legacyReceiptContextKey{}, &LegacyReceiptSet{
		receipts: map[string]FeishuLegacyReceipt{},
	})
}

func AddLegacyReceipt(ctx context.Context, r FeishuLegacyReceipt) {
	if s, ok := ctx.Value(legacyReceiptContextKey{}).(*LegacyReceiptSet); ok {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.receipts[r.ResourceID] = r
	}
}

func LegacyReceipts(ctx context.Context) []FeishuLegacyReceipt {
	s, ok := ctx.Value(legacyReceiptContextKey{}).(*LegacyReceiptSet)
	if !ok {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]FeishuLegacyReceipt, 0, len(s.receipts))
	for _, r := range s.receipts {
		out = append(out, r)
	}
	return out
}
