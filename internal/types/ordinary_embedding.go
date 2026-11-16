package types

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// OrdinaryEmbeddingScope separates ordinary file work from managed sources.
const OrdinaryEmbeddingScope = "ordinary_embedding_v1"

// OrdinaryEmbeddingPlan is server-owned durable work, never upload metadata.
// Every continuation binds one parse attempt and one exact input manifest.
type OrdinaryEmbeddingPlan struct {
	Version       int                      `json:"version"`
	Task          DocumentProcessPayload   `json:"task"`
	SourceStamp   string                   `json:"source_stamp"`
	ConfigStamp   string                   `json:"config_stamp"`
	KBStamp       string                   `json:"kb_stamp"`
	Inputs        []OrdinaryEmbeddingInput `json:"inputs"`
	Cursor        int                      `json:"cursor"`
	BatchAttempts int                      `json:"batch_attempts"`
	LeaseToken    string                   `json:"lease_token,omitempty"`
	LeaseUntil    time.Time                `json:"lease_until,omitempty"`
	StorageBytes  int64                    `json:"storage_bytes"`
	Dimension     int                      `json:"dimension"`
	LastError     string                   `json:"last_error,omitempty"`
}

// OrdinaryEmbeddingInput records the identity and immutable digest of one chunk.
type OrdinaryEmbeddingInput struct {
	ChunkID string `json:"chunk_id"`
	SHA256  string `json:"sha256"`
}

type ordinaryEmbeddingWriteKey struct{}

// WithOrdinaryEmbeddingWrite binds PG vector writes to the owning workflow
// transaction. A callback must recheck ownership under the same row locks.
func WithOrdinaryEmbeddingWrite(
	ctx context.Context,
	guard func(context.Context, *gorm.DB, func(*gorm.DB) error) error,
) context.Context {
	return context.WithValue(ctx, ordinaryEmbeddingWriteKey{}, guard)
}

// OrdinaryEmbeddingWrite invokes the SQL fence when an ordinary consumer owns the context.
func OrdinaryEmbeddingWrite(ctx context.Context, db *gorm.DB, write func(*gorm.DB) error) (bool, error) {
	guard, ok := ctx.Value(ordinaryEmbeddingWriteKey{}).(func(context.Context, *gorm.DB, func(*gorm.DB) error) error)
	if !ok {
		return false, nil
	}
	if _, _, _, exact := ExactDocumentIdentity(ctx); exact {
		return true, errors.New("ordinary embedding cannot use a managed document capability")
	}
	if _, guarded := FeishuSourceGuardFromContext(ctx); guarded {
		return true, errors.New("ordinary embedding cannot use a Feishu source capability")
	}
	return true, guard(ctx, db, write)
}
