package access

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// EnterprisePolicyStore is deliberately independent of Gin and service wiring.
type EnterprisePolicyStore interface {
	LoadEnterpriseReadSnapshot(context.Context, string, uint64, string) (*types.EnterpriseReadSnapshot, error)
	ChangeEnterprisePolicy(context.Context, types.EnterprisePolicyChange) (*types.EnterprisePolicy, error)
}

// EnterprisePolicyAuthority must check current, approved business roles. There
// is no default system-admin/tenant-admin shortcut and no production binding.
type EnterprisePolicyAuthority interface {
	AuthorizeEnterprisePolicyChange(context.Context, string, types.EnterprisePolicyTarget, string) error
}

type EnterpriseEmployeeVerifier interface {
	IsEnterpriseEmployee(context.Context, string) (bool, error)
}

type EnterpriseTextChunks interface {
	ListChunksByKnowledgeID(context.Context, uint64, string) ([]*types.Chunk, error)
}

type EnterprisePublicOptions struct {
	Enabled bool
	// Explicit new product mode. The original M2 approval evaluator remains
	// unchanged when false; no persisted approvals are synthesized.
	PortalPublishedDefault bool
}

// EnterprisePublicReader has no write, file, Wiki, agent or general KB-grant
// interface. Its zero value and all missing collaborators fail closed.
type EnterprisePublicReader struct {
	options   EnterprisePublicOptions
	store     EnterprisePolicyStore
	employees EnterpriseEmployeeVerifier
	chunks    EnterpriseTextChunks
}

func NewEnterprisePublicReader(options EnterprisePublicOptions, store EnterprisePolicyStore, employees EnterpriseEmployeeVerifier, chunks EnterpriseTextChunks) *EnterprisePublicReader {
	return &EnterprisePublicReader{options: options, store: store, employees: employees, chunks: chunks}
}

// EnterpriseReadGrant is opaque and cannot be installed as a KB/member grant.
// It must be revalidated before and after consuming parsed content.
type EnterpriseReadGrant struct {
	userID                 string
	keyID                  uint64
	tenantID               uint64
	kbID                   string
	documents              []string
	snapshot               *types.EnterpriseReadSnapshot
	portalPublishedDefault bool
}

func (g *EnterpriseReadGrant) ResourceMatches(tenant uint64, kb string) bool {
	return g != nil && g.tenantID == tenant && g.kbID == kb
}

func (g *EnterpriseReadGrant) KnowledgeIDs() []string {
	if g == nil {
		return []string{}
	}
	return append([]string{}, g.documents...)
}

// SearchTargets returns explicit document targets, never a whole-KB target.
// Empty grants produce no targets; consumers must not fall back to all KBs.
func (g *EnterpriseReadGrant) SearchTargets() types.SearchTargets {
	if g == nil || len(g.documents) == 0 {
		return types.SearchTargets{}
	}
	return types.SearchTargets{&types.SearchTarget{Type: types.SearchTargetTypeKnowledge, TenantID: g.tenantID, KnowledgeBaseID: g.kbID, KnowledgeIDs: g.KnowledgeIDs()}}
}

func approvedOrdinary(p types.EnterprisePolicy) bool {
	return p.State == types.EnterprisePolicyApproved && p.Policy == types.EnterprisePolicyInherit &&
		p.ContentClass == "ordinary" && p.SourceACLState == "local" && p.Revision > 0 &&
		p.ProposedBy != "" && p.ReviewedBy != nil && *p.ReviewedBy != "" && *p.ReviewedBy != p.ProposedBy && p.ReviewedAt != nil
}

func (r *EnterprisePublicReader) Authorize(ctx context.Context, kbID string) (*EnterpriseReadGrant, error) {
	if r == nil || !r.options.Enabled || r.store == nil || r.employees == nil || strings.TrimSpace(kbID) == "" {
		return nil, ErrForbidden
	}
	// Require the explicit authenticated context value, not a fallback guessed
	// from UserID or an execution-tenant switch.
	principal, present := ctx.Value(types.PrincipalContextKey).(types.Principal)
	caller := types.CallerFromContext(ctx)
	scope, scoped := types.TenantAPIKeyScopeFromContext(ctx)
	if !present || principal.Type != types.PrincipalWebUser || principal.ID == "" || caller.UserID != principal.ID || !scoped || scope.KeyID == 0 || scope.ScopeType != types.APIKeyScopeUserMCP || scope.FullAccess ||
		!scope.HasCapability(types.APIKeyCapabilityRetrieve) || !scope.HasCapability(types.APIKeyCapabilityEnterpriseRetrieve) {
		return nil, ErrUnauthorized
	}
	employee, err := r.employees.IsEnterpriseEmployee(ctx, principal.ID)
	if err != nil || !employee {
		return nil, ErrForbidden
	}
	var s *types.EnterpriseReadSnapshot
	if r.options.PortalPublishedDefault {
		store, ok := r.store.(DocumentPolicyStore)
		if !ok {
			return nil, ErrForbidden
		}
		s, err = store.LoadDocumentReadSnapshot(ctx, principal.ID, scope.KeyID, kbID)
	} else {
		s, err = r.store.LoadEnterpriseReadSnapshot(ctx, principal.ID, scope.KeyID, kbID)
	}
	if err != nil {
		return nil, fmt.Errorf("enterprise policy unavailable: %w", err)
	}
	if s == nil {
		return nil, ErrForbidden
	}
	key := s.Credential
	keyScope := types.TenantAPIKeyScope{ScopeType: key.ScopeType, Capabilities: key.Capabilities}
	if s.UserID != principal.ID || !s.UserActive || key.ID != scope.KeyID || key.OwnerUserID == nil || *key.OwnerUserID != principal.ID || key.ScopeType != types.APIKeyScopeUserMCP || key.FullAccess || key.RevokedAt != nil || (key.ExpiresAt != nil && !time.Now().Before(*key.ExpiresAt)) ||
		!keyScope.HasCapability(types.APIKeyCapabilityRetrieve) || !keyScope.HasCapability(types.APIKeyCapabilityEnterpriseRetrieve) {
		return nil, ErrUnauthorized
	}
	if r.options.PortalPublishedDefault {
		ids, err := portalPublishedDocuments(s)
		if err != nil || s.KBID != kbID {
			return nil, ErrForbidden
		}
		return &EnterpriseReadGrant{userID: principal.ID, keyID: scope.KeyID, tenantID: s.TenantID, kbID: kbID, documents: ids, snapshot: s, portalPublishedDefault: true}, nil
	}
	if !s.TenantActive || !s.PortalPublished || s.KBTemporary || s.KBType != "document" || s.TenantID == 0 || s.KBID != kbID || !approvedOrdinary(s.Space) || !approvedOrdinary(s.KB) || len(s.Documents) == 0 || len(s.Documents) > 2048 {
		return nil, ErrForbidden
	}
	ids := make([]string, 0, len(s.Documents))
	for _, d := range s.Documents {
		// One unreviewed/mixed/external/restricted resource HOLDS the entire
		// M2 KB. This is intentional until full document ACL is implemented.
		if d.ID == "" || d.TenantID != s.TenantID || d.KnowledgeBaseID != kbID || d.Type != "file" && d.Type != types.KnowledgeTypeManual || d.ParseStatus != types.ParseStatusCompleted || d.EnableStatus != "enabled" || d.Channel != "web" && d.Channel != "api" || d.CurrentVersion == "" || d.CurrentVersion != d.ResourceVersion || !approvedOrdinary(d.EnterprisePolicy) {
			return nil, ErrForbidden
		}
		ids = append(ids, d.ID)
	}
	return &EnterpriseReadGrant{userID: principal.ID, keyID: scope.KeyID, tenantID: s.TenantID, kbID: kbID, documents: ids, snapshot: s}, nil
}

func (r *EnterprisePublicReader) Revalidate(ctx context.Context, grant *EnterpriseReadGrant) error {
	if grant == nil {
		return ErrForbidden
	}
	fresh, err := r.Authorize(ctx, grant.kbID)
	if err != nil {
		return err
	}
	if fresh.userID != grant.userID || fresh.keyID != grant.keyID || !reflect.DeepEqual(fresh.snapshot, grant.snapshot) {
		return ErrForbidden
	}
	return nil
}

type EnterpriseParsedChunk struct {
	ID      string
	Content string
}

// ReadApprovedChunks reuses the existing text-chunk repository. It never
// switches the caller, opens files, emits attachment metadata, or calls models.
func (r *EnterprisePublicReader) ReadApprovedChunks(ctx context.Context, grant *EnterpriseReadGrant, knowledgeID string) ([]EnterpriseParsedChunk, error) {
	if r == nil || r.chunks == nil {
		return nil, ErrForbidden
	}
	if err := r.Revalidate(ctx, grant); err != nil {
		return nil, err
	}
	allowed := false
	for _, id := range grant.documents {
		if id == knowledgeID {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, ErrForbidden
	}
	chunks, err := r.chunks.ListChunksByKnowledgeID(ctx, grant.tenantID, knowledgeID)
	if err != nil {
		return nil, err
	}
	out := make([]EnterpriseParsedChunk, 0, len(chunks))
	for _, c := range chunks {
		if c == nil || c.TenantID != grant.tenantID || c.KnowledgeBaseID != grant.kbID || c.KnowledgeID != knowledgeID || c.ChunkType != types.ChunkTypeText || c.DeletedAt.Valid {
			return nil, ErrForbidden
		}
		if c.IsEnabled {
			// The M2 pilot is text-only: even a copied anonymous bearer link
			// must not escape through approved parsed text. Rich resources wait
			// for the M3 resource broker and legacy-link revocation gate.
			lower := strings.ToLower(c.Content)
			for _, marker := range []string{"http://", "https://", "resource://", "storage://", "backend://", "local://", "cos://", "s3://", "/files/", "/files?", "/r/", "!["} {
				if strings.Contains(lower, marker) {
					return nil, ErrForbidden
				}
			}
			out = append(out, EnterpriseParsedChunk{ID: c.ID, Content: c.Content})
		}
	}
	if err := r.Revalidate(ctx, grant); err != nil {
		return nil, err
	}
	return out, nil
}

// ChangeEnterprisePolicy applies server-resolved identity and mandatory role
// authorization before entering the repository's atomic CAS + audit write.
func ChangeEnterprisePolicy(ctx context.Context, store EnterprisePolicyStore, authority EnterprisePolicyAuthority, change types.EnterprisePolicyChange) (*types.EnterprisePolicy, error) {
	p, present := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if store == nil || authority == nil || !present || p.Type != types.PrincipalWebUser || p.ID == "" || types.CallerFromContext(ctx).UserID != p.ID {
		return nil, ErrUnauthorized
	}
	if _, apiKey := types.TenantAPIKeyScopeFromContext(ctx); apiKey {
		return nil, ErrForbidden
	}
	if err := authority.AuthorizeEnterprisePolicyChange(ctx, p.ID, change.Target, change.Action); err != nil {
		return nil, err
	}
	change.ActorID = p.ID
	return store.ChangeEnterprisePolicy(ctx, change)
}
