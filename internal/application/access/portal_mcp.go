package access

import (
	"context"
	"errors"
	"reflect"

	"github.com/Tencent/WeKnora/internal/perftrace"
	"github.com/Tencent/WeKnora/internal/types"
)

// PortalKBDescriptor is server-loaded metadata, never a caller's authority.
type PortalKBDescriptor struct {
	TenantID                              uint64
	ID, Name, SpaceName, EmbeddingModelID string
}
type PortalKBDirectory interface {
	ListPortalMCPCandidates(context.Context) ([]PortalKBDescriptor, error)
}

// PortalMCPResolver reuses M2 whole-KB classification and M3 private grants.
// Unknown/mixed public KBs are excluded as a unit, before any driver is called.
type PortalMCPResolver struct {
	Enabled           bool
	PublishedDefaults bool
	Directory         PortalKBDirectory
	Store             DocumentPolicyStore
	Public            *EnterprisePublicReader
	Documents         *DocumentReader
	Employees         EnterpriseEmployeeVerifier
}
type PortalKBTarget struct {
	descriptor       PortalKBDescriptor
	resolver         *PortalMCPResolver
	public           *EnterpriseReadGrant
	searches, reads  []*DocumentReadGrant
	principal        types.Principal
	caller           types.Caller
	credential       *types.TenantAPIKeyScope
	privateTenant    uint64
	privateSnapshot  *types.EnterpriseReadSnapshot
	privateDocuments []string
}

func (t *PortalKBTarget) Descriptor() PortalKBDescriptor { return t.descriptor }
func (t *PortalKBTarget) Public() bool                   { return t.public != nil }
func (t *PortalKBTarget) DocumentIDs() []string {
	if t.public != nil {
		return t.public.KnowledgeIDs()
	}
	if t.privateSnapshot != nil {
		return append([]string{}, t.privateDocuments...)
	}
	ids := []string{}
	for _, g := range t.reads {
		ids = append(ids, g.document)
	}
	return ids
}
func (r *PortalMCPResolver) Resolve(ctx context.Context, requested []string) ([]*PortalKBTarget, error) {
	defer perftrace.Measure(ctx, perftrace.Resolve)()
	ctx, diagnostics := WithPortalDiagnostics(ctx)
	if r == nil || !r.Enabled || r.Directory == nil || r.Store == nil || r.Public == nil || r.Documents == nil || r.Employees == nil {
		return nil, ErrForbidden
	}
	if !r.Documents.enabled || r.Documents.base == nil || r.Documents.store == nil {
		return nil, ErrForbidden
	}
	p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	caller := types.CallerFromContext(ctx)
	key, keyed := types.TenantAPIKeyScopeFromContext(ctx)
	if !ok || p.Type != types.PrincipalWebUser || caller.UserID != p.ID || p.ID == "" || !keyed || key.ScopeType != types.APIKeyScopeUserMCP || key.FullAccess || !key.HasCapability(types.APIKeyCapabilityRetrieve) {
		return nil, ErrUnauthorized
	}
	employee, err := r.Employees.IsEnterpriseEmployee(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if !employee {
		return nil, ErrForbidden
	}
	candidates, err := r.Directory.ListPortalMCPCandidates(ctx)
	if err != nil {
		return nil, err
	}
	if len(requested) > 128 {
		return nil, ErrForbidden
	}
	wanted := map[string]bool{}
	selected := map[string]bool{}
	for _, id := range requested {
		if id == "" {
			return nil, ErrForbidden
		}
		wanted[id] = true
		selected[id] = true
	}
	out := []*PortalKBTarget{}
candidateLoop:
	for _, kb := range candidates {
		// Requested scope is immutable. Removing a held source from the
		// outstanding checks must never turn an empty set into "all KBs".
		if len(selected) > 0 && !selected[kb.ID] {
			continue
		}
		if kb.TenantID == 0 || kb.ID == "" {
			return nil, ErrForbidden
		}
		kb.Name, err = SanitizePortalText(kb.Name)
		if err != nil {
			return nil, err
		}
		kb.SpaceName, err = SanitizePortalText(kb.SpaceName)
		if err != nil {
			return nil, err
		}
		snapshot, err := r.Store.LoadDocumentReadSnapshot(ctx, p.ID, key.KeyID, kb.ID)
		if err != nil {
			if errors.Is(err, types.ErrLegacySourceRestricted) {
				recordPortalRestriction(ctx, kb.ID)
				delete(wanted, kb.ID)
				continue candidateLoop
			}
			return nil, err
		}
		if snapshot == nil {
			return nil, ErrForbidden
		}
		if err = checkLiveDocumentCredential(snapshot, p.ID, key.KeyID, true); err != nil {
			return nil, err
		}
		if snapshot.TenantID != kb.TenantID || snapshot.KBID != kb.ID || !snapshot.UserActive || !snapshot.TenantActive || snapshot.KBTemporary || snapshot.KBType != "document" || !r.PublishedDefaults && len(snapshot.Documents) == 0 {
			continue
		}
		target := &PortalKBTarget{descriptor: kb, resolver: r, principal: p, caller: caller, credential: normalizedReadCredential(ctx)}
		if r.PublishedDefaults {
			if key.HasCapability(types.APIKeyCapabilityEnterpriseRetrieve) {
				if _, e := portalPublishedDocuments(snapshot); e == nil {
					grant, e := r.Public.Authorize(ctx, kb.ID)
					if e != nil {
						if errors.Is(e, types.ErrLegacySourceRestricted) {
							recordPortalRestriction(ctx, kb.ID)
							delete(wanted, kb.ID)
							continue candidateLoop
						}
						return nil, e
					}
					target.public = grant
				}
			}
			if target.public == nil {
				e := target.authorizePrivateSnapshot(ctx, snapshot)
				if errors.Is(e, ErrForbidden) && kb.TenantID != caller.TenantID {
					target.privateTenant = kb.TenantID
					private, x := target.privateContext(ctx)
					if x == nil {
						e = target.authorizePrivateSnapshot(private, snapshot)
					} else {
						e = x
					}
				}
				if errors.Is(e, ErrForbidden) {
					continue
				}
				if e != nil {
					if errors.Is(e, types.ErrLegacySourceRestricted) {
						recordPortalRestriction(ctx, kb.ID)
						delete(wanted, kb.ID)
						continue candidateLoop
					}
					return nil, e
				}
			}
			if e := target.Revalidate(ctx); e != nil {
				if errors.Is(e, types.ErrLegacySourceRestricted) {
					recordPortalRestriction(ctx, kb.ID)
					delete(wanted, kb.ID)
					continue candidateLoop
				}
				return nil, e
			}
			out = append(out, target)
			continue
		}
		// Avoid calling the public evaluator for clearly pending or mixed KBs. Its
		// complete version/classification checks remain authoritative for admission.
		if key.HasCapability(types.APIKeyCapabilityEnterpriseRetrieve) && snapshot.PortalPublished && approvedOrdinary(snapshot.Space) && approvedOrdinary(snapshot.KB) {
			clean := true
			for _, d := range snapshot.Documents {
				if !approvedOrdinary(d.EnterprisePolicy) || d.ResourceVersion != d.CurrentVersion {
					clean = false
					break
				}
			}
			if clean {
				g, e := r.Public.Authorize(ctx, kb.ID)
				if e != nil {
					return nil, e
				}
				target.public = g

			}
		}
		if target.public == nil {
			if len(snapshot.Documents) > MaxDocumentAllowSet {
				continue
			}
			// An independent private entitlement survives Portal withdrawal. This MVP
			// requires an entirely authorized KB; partial mixed private KBs remain HOLD.
			err := target.authorizePrivate(ctx, snapshot)
			if errors.Is(err, ErrForbidden) && kb.TenantID != caller.TenantID {
				target.privateTenant = kb.TenantID
				private, e := target.privateContext(ctx)
				if e == nil {
					err = target.authorizePrivate(private, snapshot)
				} else {
					err = e
				}
			}
			if errors.Is(err, ErrForbidden) {
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		if err := target.Revalidate(ctx); err != nil {
			return nil, err
		}
		out = append(out, target)
	}
	if len(wanted) > 0 {
		for _, t := range out {
			delete(wanted, t.descriptor.ID)
		}
		if len(wanted) > 0 {
			return nil, ErrForbidden
		}
	}
	if len(out) == 0 && diagnostics.Partial() {
		return nil, types.ErrLegacySourceRestricted
	}
	return out, nil
}
func (t *PortalKBTarget) Revalidate(ctx context.Context) error {
	if t == nil || t.resolver == nil || !t.resolver.Enabled {
		return ErrForbidden
	}
	p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if !ok || p != t.principal || !reflect.DeepEqual(types.CallerFromContext(ctx), t.caller) || !reflect.DeepEqual(normalizedReadCredential(ctx), t.credential) {
		return ErrUnauthorized
	}
	employee, err := t.resolver.Employees.IsEnterpriseEmployee(ctx, p.ID)
	if err != nil {
		return err
	}
	if !employee {
		return ErrForbidden
	}
	if t.public != nil {
		if err := t.resolver.Public.Revalidate(ctx, t.public); err != nil {
			return err
		}
		// Whole-KB public admission already verifies every document policy and
		// current version. Reuse the live KB/Key base once per operation, rather
		// than loading N whole-KB snapshots for N individual documents.
		for _, operation := range []types.DocumentOperation{types.DocumentRead, types.DocumentSearch} {
			if err := t.resolver.Documents.base.CheckDocumentReadBase(ctx, t.descriptor.TenantID, t.descriptor.ID, operation, true); err != nil {
				return err
			}
		}
		return nil
	}
	if t.privateSnapshot != nil {
		fresh, e := t.resolver.Store.LoadDocumentReadSnapshot(ctx, p.ID, t.credential.KeyID, t.descriptor.ID)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(fresh, t.privateSnapshot) {
			return ErrForbidden
		}
		if e := checkLiveDocumentCredential(fresh, p.ID, t.credential.KeyID, true); e != nil {
			return e
		}
		private, e := t.privateContext(ctx)
		if e != nil {
			return e
		}
		for _, operation := range []types.DocumentOperation{types.DocumentRead, types.DocumentSearch} {
			if e := t.resolver.Documents.base.CheckDocumentReadBase(private, t.descriptor.TenantID, t.descriptor.ID, operation, false); e != nil {
				return e
			}
		}
		return nil
	}
	if len(t.reads) == 0 || len(t.searches) != len(t.reads) {
		return ErrForbidden
	}
	private, err := t.privateContext(ctx)
	if err != nil {
		return err
	}
	for _, g := range append(append([]*DocumentReadGrant{}, t.reads...), t.searches...) {
		if err := g.Revalidate(private); err != nil {
			return err
		}
	}
	return nil
}

func (t *PortalKBTarget) privateContext(ctx context.Context) (context.Context, error) {
	if t.privateTenant == 0 {
		return ctx, nil
	}
	base, ok := t.resolver.Documents.base.(interface {
		PrivateWorkspaceContext(context.Context, uint64) (context.Context, error)
	})
	if !ok {
		return nil, ErrForbidden
	}
	return base.PrivateWorkspaceContext(ctx, t.privateTenant)
}
func (t *PortalKBTarget) authorizePrivate(ctx context.Context, snapshot *types.EnterpriseReadSnapshot) error {
	t.reads = nil
	t.searches = nil
	for _, d := range snapshot.Documents {
		read, err := t.resolver.Documents.Authorize(ctx, t.descriptor.ID, d.ID, types.DocumentRead)
		if err != nil {
			return err
		}
		search, err := t.resolver.Documents.Authorize(ctx, t.descriptor.ID, d.ID, types.DocumentSearch)
		if err != nil {
			return err
		}
		if read.public || search.public {
			return ErrForbidden
		}
		t.reads = append(t.reads, read)
		t.searches = append(t.searches, search)
	}
	return nil
}

func (t *PortalKBTarget) authorizePrivateSnapshot(ctx context.Context, snapshot *types.EnterpriseReadSnapshot) error {
	for _, operation := range []types.DocumentOperation{types.DocumentRead, types.DocumentSearch} {
		if err := t.resolver.Documents.base.CheckDocumentReadBase(ctx, t.descriptor.TenantID, t.descriptor.ID, operation, false); err != nil {
			return err
		}
	}
	ids, err := portalEffectiveDocuments(snapshot)
	if err != nil {
		return err
	}
	t.privateSnapshot = snapshot
	t.privateDocuments = ids
	return nil
}

type portalRetrievalKey struct{}
type portalRetrievalScope struct {
	target *PortalKBTarget
}

func (t *PortalKBTarget) SearchContext(ctx context.Context) (context.Context, error) {
	if err := t.Revalidate(ctx); err != nil {
		return nil, err
	}
	return context.WithValue(types.WithExecutionTenant(ctx, t.descriptor.TenantID), portalRetrievalKey{}, portalRetrievalScope{target: t}), nil
}

// GuardPortalRetrieval is consumed by the real PostgreSQL entrypoints before
// SQL. Unknown drivers cannot consume this opaque scope. IDs only intersect.
func GuardPortalRetrieval(ctx context.Context, engine types.RetrieverEngineType, params types.RetrieveParams) (types.RetrieveParams, error) {
	s, present := ctx.Value(portalRetrievalKey{}).(portalRetrievalScope)
	if !present {
		return params, nil
	}
	if s.target == nil || engine != types.PostgresRetrieverEngineType || params.TopK < 1 || params.TopK > 100 || len(params.Query) > 4096 || len(params.KnowledgeIDs) > MaxDocumentAllowSet || len(params.TagIDs) > 0 || params.AdditionalParams != nil {
		return types.RetrieveParams{}, ErrDocumentFilterDenied
	}
	tenant, ok := types.TenantIDFromContext(ctx)
	if !ok || tenant != s.target.descriptor.TenantID {
		return types.RetrieveParams{}, ErrDocumentFilterDenied
	}
	if err := s.target.Revalidate(ctx); err != nil {
		return types.RetrieveParams{}, err
	}
	if s.target.public != nil || s.target.privateSnapshot != nil {
		for _, id := range params.KnowledgeBaseIDs {
			if id != s.target.descriptor.ID {
				return types.RetrieveParams{}, ErrDocumentFilterDenied
			}
		}
		for _, id := range params.KnowledgeIDs {
			found := false
			for _, allowed := range s.target.DocumentIDs() {
				if id == allowed {
					found = true
					break
				}
			}
			if !found {
				return types.RetrieveParams{}, ErrDocumentFilterDenied
			}
		}
		// Empty document IDs here mean the certified whole-KB SQL relation,
		// exclusively within this opaque PostgreSQL scope. Never passed to any
		// unsupported driver or reused as a legacy empty allow-set.
		params.KnowledgeBaseIDs = []string{s.target.descriptor.ID}
		return params, nil
	}
	set := AuthorizedDocumentSet{tenantID: tenant, kbID: s.target.descriptor.ID, ids: s.target.DocumentIDs()}
	return set.Constrain(tenant, s.target.descriptor.ID, params, true)
}
func PortalRetrievalTenant(ctx context.Context) (uint64, bool) {
	s, ok := ctx.Value(portalRetrievalKey{}).(portalRetrievalScope)
	if !ok || s.target == nil {
		return 0, false
	}
	return s.target.descriptor.TenantID, true
}

func PortalRetrievalPublic(ctx context.Context) bool {
	s, ok := ctx.Value(portalRetrievalKey{}).(portalRetrievalScope)
	return ok && s.target != nil && s.target.public != nil
}

func PortalRetrievalHasDocuments(ctx context.Context) bool {
	s, ok := ctx.Value(portalRetrievalKey{}).(portalRetrievalScope)
	return ok && s.target != nil && len(s.target.DocumentIDs()) > 0
}

func PortalRetrievalPublishedDefaults(ctx context.Context) bool {
	s, ok := ctx.Value(portalRetrievalKey{}).(portalRetrievalScope)
	return ok && s.target != nil && s.target.resolver.PublishedDefaults
}
