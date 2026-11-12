package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// Protocol credentials authorize transport and schema discovery only. They
// cannot substitute for a document, public-read or content-creation grant.
type portalProtocolCredentialVerifier interface {
	VerifyPortalMCPProtocolCredential(context.Context, string, uint64, uint64) error
}

// AuthorizeProtocol keeps idle streams and protocol discovery independent of
// corpus size and document revisions. Every tools/call retains its own live
// knowledge authorization; no knowledge names or contents are returned here.
func (r *PortalMCPResolver) AuthorizeProtocol(ctx context.Context) error {
	if r == nil || !r.Enabled || r.Directory == nil || r.Store == nil || r.Public == nil ||
		r.Documents == nil || !r.Documents.enabled || r.Documents.base == nil ||
		r.Documents.store == nil || r.Employees == nil {
		return ErrForbidden
	}
	p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	caller := types.CallerFromContext(ctx)
	key, keyed := types.TenantAPIKeyScopeFromContext(ctx)
	if !ok || p.Type != types.PrincipalWebUser || p.ID == "" || caller.UserID != p.ID ||
		caller.TenantID == 0 || !keyed || key.KeyID == 0 || key.ScopeType != types.APIKeyScopeUserMCP ||
		key.FullAccess || !key.HasCapability(types.APIKeyCapabilityRetrieve) {
		return ErrUnauthorized
	}
	verifier, fast := r.Store.(portalProtocolCredentialVerifier)
	if !fast {
		// Preserve the original fail-closed contract of alternative stores.
		_, err := r.Resolve(ctx, nil)
		return err
	}
	active, err := r.Employees.IsEnterpriseEmployee(ctx, p.ID)
	if err != nil {
		return err
	}
	if !active {
		return ErrForbidden
	}
	return verifier.VerifyPortalMCPProtocolCredential(ctx, p.ID, key.KeyID, caller.TenantID)
}
