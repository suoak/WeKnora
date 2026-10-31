package types

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

var ErrExactDocumentScope = errors.New("exact_document_scope_unverified")

type exactDocumentScopeKey struct{}

// ExactDocumentScope is an in-process, server-issued capability. It is not
// serialized into a task, header or metadata. Reads are limited to one document;
// KB bodies and unrelated documents retain the legacy protection.
type ExactDocumentScope struct {
	TenantID    uint64
	KnowledgeID string
	KBID        string
	Read        func(context.Context) error
	Commit      func(*gorm.DB) error
}

// WithExactDocumentScope attaches the verified worker authority. Source and
// binding validation must precede issuance; each use revalidates live ownership.
func WithExactDocumentScope(ctx context.Context, scope ExactDocumentScope) (context.Context, error) {
	if scope.TenantID == 0 || scope.KnowledgeID == "" || scope.KBID == "" || scope.Read == nil || scope.Commit == nil {
		return ctx, ErrExactDocumentScope
	}
	if err := scope.Read(ctx); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, exactDocumentScopeKey{}, scope), nil
}

// CheckExactDocumentRead never grants a whole-KB read. CheckPrivateKBRead must
// still be applied by the caller before this narrower source proof is consumed.
func CheckExactDocumentRead(ctx context.Context, knowledge, kb string) (bool, error) {
	scope, ok := ctx.Value(exactDocumentScopeKey{}).(ExactDocumentScope)
	if !ok {
		return false, nil
	}
	if knowledge == "" || knowledge != scope.KnowledgeID || kb != scope.KBID {
		return false, nil
	}
	if scope.Read == nil || scope.Commit == nil {
		return true, ErrExactDocumentScope
	}
	return true, scope.Read(ctx)
}

// CheckExactDocumentCommit is called inside the repository's actual write
// transaction, so generation/nonce/grant locks cover the write itself.
func CheckExactDocumentCommit(tx *gorm.DB, tenant uint64, knowledge, kb string) (bool, error) {
	scope, ok := tx.Statement.Context.Value(exactDocumentScopeKey{}).(ExactDocumentScope)
	if !ok {
		return false, nil
	}
	if tenant != scope.TenantID || knowledge != scope.KnowledgeID || kb != scope.KBID || scope.Commit == nil {
		return true, ErrExactDocumentScope
	}
	return true, scope.Commit(tx)
}

// ExactDocumentIdentity exposes only the target, never a credential or source
// body. Vector repositories have no tenant column and use this target together
// with their actual knowledge/KB IDs when checking the write transaction.
func ExactDocumentIdentity(ctx context.Context) (uint64, string, string, bool) {
	scope, ok := ctx.Value(exactDocumentScopeKey{}).(ExactDocumentScope)
	return scope.TenantID, scope.KnowledgeID, scope.KBID, ok
}

// WithExactDocumentWrite keeps the live protocol locks until the data write
// commits. Ordinary callers retain their original path and semantics.
func WithExactDocumentWrite(
	ctx context.Context, db *gorm.DB, tenant uint64, knowledge, kb string, write func(*gorm.DB) error,
) error {
	if _, _, _, exact := ExactDocumentIdentity(ctx); !exact {
		return write(db.WithContext(ctx))
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := CheckExactDocumentCommit(tx, tenant, knowledge, kb); err != nil {
			return err
		}
		if err := write(tx); err != nil {
			return err
		}
		_, err := CheckExactDocumentCommit(tx, tenant, knowledge, kb)
		return err
	})
}
