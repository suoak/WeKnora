package types

import (
	"context"
	"errors"
	"time"
)

// ErrWikiClaimChanged fences settlement by a superseded Wiki consumer.
var ErrWikiClaimChanged = errors.New("wiki operation claim ownership changed")

type wikiClaimOwnerKey struct{}

// WikiClaimOwner binds metadata settlement to the exact SQL leases acquired.
type WikiClaimOwner struct {
	TenantID uint64
	KBID     string
	Claims   map[int64]time.Time
}

// WithWikiClaimOwner captures immutable ownership for pending-operation writes.
func WithWikiClaimOwner(ctx context.Context, tenant uint64, kb string, claims map[int64]time.Time) context.Context {
	copyClaims := make(map[int64]time.Time, len(claims))
	for id, stamp := range claims {
		copyClaims[id] = stamp
	}
	return context.WithValue(ctx, wikiClaimOwnerKey{}, WikiClaimOwner{tenant, kb, copyClaims})
}

// WikiClaimOwnerFromContext returns a private copy, never a mutable capability.
func WikiClaimOwnerFromContext(ctx context.Context) (WikiClaimOwner, bool) {
	owner, ok := ctx.Value(wikiClaimOwnerKey{}).(WikiClaimOwner)
	if !ok {
		return WikiClaimOwner{}, false
	}
	claims := make(map[int64]time.Time, len(owner.Claims))
	for id, stamp := range owner.Claims {
		claims[id] = stamp
	}
	owner.Claims = claims
	return owner, true
}
