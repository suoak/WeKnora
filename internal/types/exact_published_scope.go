package types

import (
	"context"
	"sync/atomic"

	"gorm.io/gorm"
)

// ReadExactPublishedPage validates a bounded repository read before selecting
// any body and again before returning it. Row hooks retain the exact identity,
// without performing remote OAuth requests for every row in the same page.
// The temporary capability expires when read returns and never grants writes.
func ReadExactPublishedPage(ctx context.Context, knowledge string, read func(context.Context) error) error {
	proof, exact := ctx.Value(exactPublishedScopeKey{}).(exactPublishedScope)
	if !exact {
		return read(ctx)
	}
	if knowledge != proof.scope.KnowledgeID {
		return ErrExactDocumentScope
	}
	if _, err := CheckExactPublishedDocumentRead(ctx); err != nil {
		return err
	}
	var active atomic.Bool
	active.Store(true)
	defer active.Store(false)
	caller := CallerFromContext(ctx)
	principal, principalOK := ctx.Value(PrincipalContextKey).(Principal)
	scope := proof.scope
	scope.Commit = func(*gorm.DB) error { return ErrExactDocumentScope }
	scope.Read = func(current context.Context) error {
		tenant, ok := TenantIDFromContext(current)
		currentPrincipal, currentPrincipalOK := current.Value(PrincipalContextKey).(Principal)
		if !active.Load() || !ok || tenant != scope.TenantID ||
			CallerFromContext(
				current,
			) != caller || !principalOK || !currentPrincipalOK || currentPrincipal != principal {
			return ErrExactDocumentScope
		}
		return current.Err()
	}
	page := context.WithValue(ctx, exactDocumentScopeKey{}, scope)
	if err := read(page); err != nil {
		return err
	}
	_, err := CheckExactPublishedDocumentRead(ctx)
	return err
}

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

// ExactPublishedDocumentIdentity returns the bound receipt and document identity.
func ExactPublishedDocumentIdentity(ctx context.Context) (int64, uint64, string, string, bool) {
	proof, ok := ctx.Value(exactPublishedScopeKey{}).(exactPublishedScope)
	return proof.receipt, proof.scope.TenantID, proof.scope.KnowledgeID, proof.scope.KBID, ok
}

// CheckExactPublishedDocumentRead revalidates the current read-only capability.
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
