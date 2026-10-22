package access

import (
	"context"
	"reflect"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

type DocumentPolicyStore interface {
	LoadDocumentReadSnapshot(context.Context, string, uint64, string) (*types.EnterpriseReadSnapshot, error)
	LoadDocumentSubjectGrant(context.Context, string, string, types.DocumentOperation) (*types.DocumentSubjectGrant, error)
	ChangeDocumentSubjectGrant(context.Context, types.DocumentGrantChange) (*types.DocumentSubjectGrant, error)
}

// DocumentBaseVerifier must recheck existing live KB/share/membership and Key
// scope facts, not trust an old context grant. Enterprise reads do not require
// target membership; they still require the separately approved credential.
// File operations additionally require the existing file ACL and proof that
// legacy bearer URLs/caches are contained. No implementation means HOLD.
type DocumentBaseVerifier interface {
	CheckDocumentReadBase(context.Context, uint64, string, types.DocumentOperation, bool) error
}

type DocumentGrantAuthority interface {
	AuthorizeDocumentGrantChange(context.Context, string, types.DocumentGrantChange) error
}

type DocumentReader struct {
	enabled   bool
	store     DocumentPolicyStore
	base      DocumentBaseVerifier
	employees EnterpriseEmployeeVerifier
}

func NewDocumentReader(enabled bool, store DocumentPolicyStore, base DocumentBaseVerifier, employees EnterpriseEmployeeVerifier) *DocumentReader {
	return &DocumentReader{enabled: enabled, store: store, base: base, employees: employees}
}

// DocumentReadGrant is opaque and scoped to one principal, credential, resource
// and operation. It conveys neither membership nor a generic KB grant.
type DocumentReadGrant struct {
	reader            *DocumentReader
	user              string
	key               uint64
	tenant            uint64
	kb, document      string
	operation         types.DocumentOperation
	caller            types.Caller
	credentialContext *types.TenantAPIKeyScope
	public            bool
	snapshot          *types.EnterpriseReadSnapshot
	subject           *types.DocumentSubjectGrant
}

func (r *DocumentReader) Authorize(ctx context.Context, kb, doc string, operation types.DocumentOperation) (*DocumentReadGrant, error) {
	if r == nil || !r.enabled || r.store == nil || r.base == nil || !operation.Valid() || kb == "" || doc == "" {
		return nil, ErrForbidden
	}
	p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if !ok || p.Type != types.PrincipalWebUser || p.ID == "" || types.CallerFromContext(ctx).UserID != p.ID {
		return nil, ErrUnauthorized
	}
	scope, keyed := types.TenantAPIKeyScopeFromContext(ctx)
	if keyed && (scope.KeyID == 0 || scope.ScopeType != types.APIKeyScopeUserMCP || scope.FullAccess || !scope.HasCapability(types.APIKeyCapabilityRetrieve)) {
		return nil, ErrForbidden
	}
	keyID := uint64(0)
	if keyed {
		keyID = scope.KeyID
	}
	s, err := r.store.LoadDocumentReadSnapshot(ctx, p.ID, keyID, kb)
	if err != nil {
		return nil, err
	}
	if s == nil || s.UserID != p.ID || !s.UserActive || !s.TenantActive || s.TenantID == 0 || s.KBID != kb || s.KBTemporary || s.KBType != "document" {
		return nil, ErrForbidden
	}
	if err := checkLiveDocumentCredential(s, p.ID, keyID, keyed); err != nil {
		return nil, err
	}

	var d *types.EnterpriseDocumentState
	for i := range s.Documents {
		if s.Documents[i].ID == doc {
			d = &s.Documents[i]
			break
		}
	}
	if d == nil || d.TenantID != s.TenantID || d.KnowledgeBaseID != kb || d.CurrentVersion == "" || d.CurrentVersion != d.ResourceVersion || d.State != "approved" || d.Revision <= 0 || d.ReviewedBy == nil || *d.ReviewedBy == d.ProposedBy || d.ReviewedAt == nil || d.ProposedBy == "" || d.Type != "file" && d.Type != types.KnowledgeTypeManual || d.ParseStatus != types.ParseStatusCompleted || d.EnableStatus != "enabled" || d.Channel != "web" && d.Channel != "api" || d.SourceACLState != "local" && d.SourceACLState != "verified" {
		return nil, ErrForbidden
	}
	// There is no trustworthy way to distinguish old Python from direct REST
	// with the same Key. All API-key reads obey mcp_blocked, regardless of UA
	// or X-From-MCP. Web reads still need their own entitlement below.
	if d.Policy == types.EnterprisePolicyDeny || d.Policy == types.EnterprisePolicyPending || d.Policy == types.EnterprisePolicyMCPBlocked && keyed {
		return nil, ErrForbidden
	}
	if d.ContentClass != "ordinary" && d.ContentClass != "mixed" || d.Policy == types.EnterprisePolicyInherit && d.ContentClass != "ordinary" {
		return nil, ErrForbidden
	}
	// Source permission revalidation is not yet wired to Feishu/GitLab.
	// A historical "verified" flag is not proof of current source rights.
	if d.SourceACLState != "local" {
		return nil, ErrForbidden
	}
	public := false
	if operation != types.DocumentFile && d.Policy == types.EnterprisePolicyInherit && approvedOrdinary(d.EnterprisePolicy) && approvedOrdinary(s.Space) && approvedOrdinary(s.KB) && s.PortalPublished && r.employees != nil {
		employee, e := r.employees.IsEnterpriseEmployee(ctx, p.ID)
		if e != nil {
			return nil, e
		}
		public = employee && (!keyed || scope.HasCapability(types.APIKeyCapabilityEnterpriseRetrieve) && (types.TenantAPIKeyScope{Capabilities: s.Credential.Capabilities}).HasCapability(types.APIKeyCapabilityEnterpriseRetrieve))
	}
	if d.Policy != types.EnterprisePolicyInherit && d.Policy != types.EnterprisePolicyRestricted && d.Policy != types.EnterprisePolicyMCPBlocked {
		return nil, ErrForbidden
	}
	var subject *types.DocumentSubjectGrant
	if !public && (d.Policy == types.EnterprisePolicyRestricted || d.Policy == types.EnterprisePolicyMCPBlocked) {
		subject, err = r.store.LoadDocumentSubjectGrant(ctx, doc, p.ID, operation)
		if err != nil {
			return nil, err
		}
		if subject == nil || subject.KnowledgeID != doc || subject.UserID != p.ID || subject.Operation != operation || subject.State != "approved" || subject.Revision <= 0 || subject.ResourceVersion != d.CurrentVersion {
			return nil, ErrForbidden
		}
	}
	if err := r.base.CheckDocumentReadBase(ctx, s.TenantID, kb, operation, public); err != nil {
		return nil, err
	}
	var credentialContext *types.TenantAPIKeyScope
	if keyed {
		copy := scope.Normalize()
		copy.KnowledgeBaseIDs = append(types.StringArray(nil), copy.KnowledgeBaseIDs...)
		copy.Capabilities = append(types.StringArray(nil), copy.Capabilities...)
		credentialContext = &copy
	}
	return &DocumentReadGrant{reader: r, user: p.ID, key: keyID, tenant: s.TenantID, kb: kb, document: doc, operation: operation, snapshot: s, subject: subject, caller: types.CallerFromContext(ctx), credentialContext: credentialContext, public: public}, nil
}

func (g *DocumentReadGrant) Revalidate(ctx context.Context) error {
	if g == nil || g.reader == nil {
		return ErrForbidden
	}
	fresh, err := g.reader.Authorize(ctx, g.kb, g.document, g.operation)
	if err != nil {
		return err
	}
	if fresh.user != g.user || fresh.key != g.key || fresh.tenant != g.tenant || fresh.caller != g.caller || fresh.public != g.public || !reflect.DeepEqual(fresh.credentialContext, g.credentialContext) || !reflect.DeepEqual(fresh.snapshot, g.snapshot) || !reflect.DeepEqual(fresh.subject, g.subject) {
		return ErrForbidden
	}
	return nil
}

func ChangeDocumentSubjectGrant(ctx context.Context, store DocumentPolicyStore, authority DocumentGrantAuthority, c types.DocumentGrantChange) (*types.DocumentSubjectGrant, error) {
	p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if store == nil || authority == nil || !ok || p.Type != types.PrincipalWebUser || p.ID == "" || types.CallerFromContext(ctx).UserID != p.ID {
		return nil, ErrUnauthorized
	}
	if _, keyed := types.TenantAPIKeyScopeFromContext(ctx); keyed {
		return nil, ErrForbidden
	}
	c.ActorID = p.ID
	if err := authority.AuthorizeDocumentGrantChange(ctx, p.ID, c); err != nil {
		return nil, err
	}
	return store.ChangeDocumentSubjectGrant(ctx, c)
}

// ReadText never returns file/preview/image metadata. Each byte is from the
// canonical current Chunk repository, with live checks before and after I/O.
func (r *DocumentReader) ReadText(ctx context.Context, g *DocumentReadGrant, chunks EnterpriseTextChunks) ([]EnterpriseParsedChunk, error) {
	if g == nil || g.reader != r || g.operation != types.DocumentRead || chunks == nil {
		return nil, ErrForbidden
	}
	if err := g.Revalidate(ctx); err != nil {
		return nil, err
	}
	rows, err := chunks.ListChunksByKnowledgeID(ctx, g.tenant, g.document)
	if err != nil {
		return nil, err
	}
	out := make([]EnterpriseParsedChunk, 0, len(rows))
	for _, c := range rows {
		if c == nil || c.TenantID != g.tenant || c.KnowledgeBaseID != g.kb || c.KnowledgeID != g.document || c.DeletedAt.Valid || c.ChunkType != types.ChunkTypeText {
			return nil, ErrForbidden
		}
		if c.IsEnabled {
			if !safeEnterpriseText(c.Content) {
				return nil, ErrForbidden
			}
			out = append(out, EnterpriseParsedChunk{ID: c.ID, Content: c.Content})
		}
	}
	if err := g.Revalidate(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func checkLiveDocumentCredential(s *types.EnterpriseReadSnapshot, user string, keyID uint64, keyed bool) error {
	if keyed {
		k := s.Credential
		caps := types.TenantAPIKeyScope{Capabilities: k.Capabilities}
		if k.ID != keyID || k.OwnerUserID == nil || *k.OwnerUserID != user || k.ScopeType != types.APIKeyScopeUserMCP || k.FullAccess || k.RevokedAt != nil || k.ExpiresAt != nil && !time.Now().Before(*k.ExpiresAt) || !caps.HasCapability(types.APIKeyCapabilityRetrieve) {
			return ErrUnauthorized
		}
	}
	return nil
}
