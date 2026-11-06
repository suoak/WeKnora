package types

import (
	"context"
	"sync/atomic"

	"gorm.io/gorm"
)

// ReadExactWorkerPage brackets a bounded metadata/body read with the live
// worker proof. Its temporary row authority cannot escape the callback, grant
// writes, or turn a published viewer capability into a worker capability.
// Model calls and publication continue to use the original live proof.
func ReadExactWorkerPage(
	ctx context.Context, tenant uint64, knowledge, kb string, read func(context.Context) error,
) error {
	scope, ok := ctx.Value(exactDocumentScopeKey{}).(ExactDocumentScope)
	if _, _, _, _, published := ExactPublishedDocumentIdentity(ctx); published || !ok {
		return ErrExactDocumentScope
	}
	boundTenant, valid := TenantIDFromContext(ctx)
	if !valid || tenant != boundTenant || tenant != scope.TenantID ||
		knowledge != scope.KnowledgeID || kb != scope.KBID || scope.Read == nil || scope.Commit == nil {
		return ErrExactDocumentScope
	}
	if err := scope.Read(ctx); err != nil {
		return err
	}
	var active atomic.Bool
	active.Store(true)
	defer active.Store(false)
	caller := CallerFromContext(ctx)
	principal, principalOK := ctx.Value(PrincipalContextKey).(Principal)
	page := scope
	page.Commit = func(*gorm.DB) error { return ErrExactDocumentScope }
	page.Read = func(current context.Context) error {
		currentTenant, valid := TenantIDFromContext(current)
		currentPrincipal, currentPrincipalOK := current.Value(PrincipalContextKey).(Principal)
		if !active.Load() || !valid || currentTenant != tenant || CallerFromContext(current) != caller ||
			currentPrincipalOK != principalOK || currentPrincipal != principal {
			return ErrExactDocumentScope
		}
		return current.Err()
	}
	if err := read(context.WithValue(ctx, exactDocumentScopeKey{}, page)); err != nil {
		return err
	}
	return scope.Read(ctx)
}
