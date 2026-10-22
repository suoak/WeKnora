package access

import (
	"context"
	"errors"
	"reflect"

	"github.com/Tencent/WeKnora/internal/types"
)

// DocumentReadScope contains only server-issued, live document read grants.
// An empty scope is DENY_ALL. It never means the legacy repository ALL scope.
type DocumentReadScope struct {
	reader     *DocumentReader
	tenant     uint64
	kb         string
	grants     []*DocumentReadGrant
	principal  types.Principal
	caller     types.Caller
	credential *types.TenantAPIKeyScope
}
type documentReadScopeKey struct{}

func (r *DocumentReader) AuthorizeKBReads(ctx context.Context, kb string) (*DocumentReadScope, error) {
	if r == nil || !r.enabled || r.store == nil || r.base == nil || kb == "" {
		return nil, ErrForbidden
	}
	p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	caller := types.CallerFromContext(ctx)
	if !ok || p.Type != types.PrincipalWebUser || p.ID == "" || caller.UserID != p.ID {
		return nil, ErrUnauthorized
	}
	keyID := uint64(0)
	if key, exists := types.TenantAPIKeyScopeFromContext(ctx); exists {
		if key.KeyID == 0 || key.ScopeType != types.APIKeyScopeUserMCP || key.FullAccess || !key.HasCapability(types.APIKeyCapabilityRetrieve) {
			return nil, ErrForbidden
		}
		keyID = key.KeyID
	}
	snapshot, err := r.store.LoadDocumentReadSnapshot(ctx, p.ID, keyID, kb)
	if err != nil {
		return nil, err
	}
	if snapshot == nil || snapshot.UserID != p.ID || !snapshot.UserActive || !snapshot.TenantActive || snapshot.TenantID == 0 || snapshot.KBID != kb || len(snapshot.Documents) > MaxDocumentAllowSet {
		return nil, ErrForbidden
	}
	if err := checkLiveDocumentCredential(snapshot, p.ID, keyID, keyID != 0); err != nil {
		return nil, err
	}
	scope := &DocumentReadScope{reader: r, tenant: snapshot.TenantID, kb: kb, principal: p, caller: caller}
	if key, exists := types.TenantAPIKeyScopeFromContext(ctx); exists {
		copy := key.Normalize()
		copy.KnowledgeBaseIDs = append(types.StringArray(nil), copy.KnowledgeBaseIDs...)
		copy.Capabilities = append(types.StringArray(nil), copy.Capabilities...)
		scope.credential = &copy
	}
	for _, doc := range snapshot.Documents {
		grant, err := r.Authorize(ctx, kb, doc.ID, types.DocumentRead)
		if errors.Is(err, ErrForbidden) {
			continue
		}
		if err != nil {
			return nil, err
		}
		scope.grants = append(scope.grants, grant)
	}
	return scope, nil
}

func (s *DocumentReadScope) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, documentReadScopeKey{}, s)
}
func (s *DocumentReadScope) TenantID() uint64 {
	if s == nil {
		return 0
	}
	return s.tenant
}
func (s *DocumentReadScope) Validate(ctx context.Context) error {
	if s == nil || s.reader == nil || s.tenant == 0 {
		return ErrForbidden
	}
	if p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal); !ok || !reflect.DeepEqual(p, s.principal) || !reflect.DeepEqual(types.CallerFromContext(ctx), s.caller) {
		return ErrUnauthorized
	}
	var credential *types.TenantAPIKeyScope
	if key, exists := types.TenantAPIKeyScopeFromContext(ctx); exists {
		copy := key.Normalize()
		copy.KnowledgeBaseIDs = append(types.StringArray(nil), copy.KnowledgeBaseIDs...)
		copy.Capabilities = append(types.StringArray(nil), copy.Capabilities...)
		credential = &copy
	}
	if !reflect.DeepEqual(credential, s.credential) {
		return ErrUnauthorized
	}
	if len(s.grants) == 0 {
		keyID := uint64(0)
		if credential != nil {
			keyID = credential.KeyID
		}
		snapshot, err := s.reader.store.LoadDocumentReadSnapshot(ctx, s.principal.ID, keyID, s.kb)
		if err != nil {
			return err
		}
		if snapshot == nil || !snapshot.UserActive || !snapshot.TenantActive || snapshot.TenantID != s.tenant || snapshot.KBID != s.kb {
			return ErrForbidden
		}
		if err = checkLiveDocumentCredential(snapshot, s.principal.ID, keyID, credential != nil); err != nil {
			return err
		}
	}
	for _, grant := range s.grants {
		if err := grant.Revalidate(ctx); err != nil {
			return err
		}
	}
	return nil
}
func GuardDocumentReadQuery(ctx context.Context, tenant uint64, kb string) ([]string, bool, error) {
	scope, present := ctx.Value(documentReadScopeKey{}).(*DocumentReadScope)
	if !present {
		return nil, false, nil
	}
	if scope == nil || tenant != scope.tenant || kb != scope.kb {
		return nil, true, ErrForbidden
	}
	if err := scope.Validate(ctx); err != nil {
		return nil, true, err
	}
	ids := make([]string, 0, len(scope.grants))
	for _, grant := range scope.grants {
		ids = append(ids, grant.document)
	}
	return ids, true, nil
}

// SafeDocumentText applies the same resource-link containment contract used by
// existing DocumentReader.ReadText. Storage metadata and raw files stay HOLD.
func SafeDocumentText(text string) bool { return safeEnterpriseText(text) }
