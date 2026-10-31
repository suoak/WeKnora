package types

import (
	"context"

	"gorm.io/gorm"
)

type exactPublishedScopeKey struct{}

type exactPublishedScope struct {
	receipt int64
	scope   ExactDocumentScope
}

// WithExactPublishedDocumentRead is a server-issued, read-only capability.
// The issuer must independently verify the viewer, live source, published
// version and database output fingerprints. Worker scopes never imply this.
func WithExactPublishedDocumentRead(
	ctx context.Context, receipt int64, scope ExactDocumentScope,
) (context.Context, error) {
	if receipt <= 0 {
		return ctx, ErrExactDocumentScope
	}
	scope.Commit = func(*gorm.DB) error { return ErrExactDocumentScope }
	guarded, err := WithExactDocumentScope(ctx, scope)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(guarded, exactPublishedScopeKey{}, exactPublishedScope{receipt, scope}), nil
}

func ExactPublishedDocumentIdentity(ctx context.Context) (int64, uint64, string, string, bool) {
	proof, ok := ctx.Value(exactPublishedScopeKey{}).(exactPublishedScope)
	return proof.receipt, proof.scope.TenantID, proof.scope.KnowledgeID, proof.scope.KBID, ok
}

func CheckExactPublishedDocumentRead(ctx context.Context) (bool, error) {
	proof, ok := ctx.Value(exactPublishedScopeKey{}).(exactPublishedScope)
	if !ok {
		return false, nil
	}
	tenant, valid := TenantIDFromContext(ctx)
	boundTenant, boundKnowledge, boundKB, bound := ExactDocumentIdentity(ctx)
	if !valid || tenant != proof.scope.TenantID || proof.receipt <= 0 || proof.scope.Read == nil {
		return true, ErrExactDocumentScope
	}
	if !bound || boundTenant != tenant || boundKnowledge != proof.scope.KnowledgeID || boundKB != proof.scope.KBID {
		return true, ErrExactDocumentScope
	}
	return true, proof.scope.Read(ctx)
}
