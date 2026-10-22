package access

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

const MaxDocumentAllowSet = 256

type documentSearchContextKey struct{}
type documentSearchScope struct {
	grants []*DocumentReadGrant
	tenant uint64
	kb     string
}

// WithDocumentSearch carries opaque live leases. An empty slice is DENY_ALL.
// No header, metadata, request filter or exported string key can mint a scope.
func WithDocumentSearch(ctx context.Context, grants ...*DocumentReadGrant) context.Context {
	s := documentSearchScope{grants: append([]*DocumentReadGrant(nil), grants...)}
	if len(grants) > 0 && grants[0] != nil {
		s.tenant = grants[0].tenant
		s.kb = grants[0].kb
	}
	return context.WithValue(ctx, documentSearchContextKey{}, s)
}

func GuardDocumentRetrieval(ctx context.Context, engine types.RetrieverEngineType, params types.RetrieveParams) (types.RetrieveParams, error) {
	var portalErr error
	params, portalErr = GuardPortalRetrieval(ctx, engine, params)
	if portalErr != nil {
		return types.RetrieveParams{}, portalErr
	}
	s, present := ctx.Value(documentSearchContextKey{}).(documentSearchScope)
	if !present {
		return params, nil
	} // Existing unenabled application is unchanged.
	// Only the actual SQLite driver has this batch's canonical SQL gate.
	executionTenant, validTenant := types.TenantIDFromContext(ctx)
	if engine != types.SQLiteRetrieverEngineType || len(s.grants) == 0 || len(s.grants) > MaxDocumentAllowSet || s.tenant == 0 || s.kb == "" || !validTenant || executionTenant != s.tenant {
		return types.RetrieveParams{}, ErrDocumentFilterDenied
	}
	if params.TopK <= 0 || params.TopK > 500 || len(params.Query) > 4096 || params.RetrieverType != "" && params.RetrieverType != types.KeywordsRetrieverType && params.RetrieverType != types.VectorRetrieverType {
		return types.RetrieveParams{}, ErrDocumentFilterDenied
	}
	ids := make([]string, 0, len(s.grants))
	for _, g := range s.grants {
		if g == nil || g.tenant != s.tenant || g.kb != s.kb || g.operation != types.DocumentSearch {
			return types.RetrieveParams{}, ErrDocumentFilterDenied
		}
		if err := g.Revalidate(ctx); err != nil {
			return types.RetrieveParams{}, err
		}
		ids = append(ids, g.document)
	}
	set := AuthorizedDocumentSet{tenantID: s.tenant, kbID: s.kb, ids: ids}
	return set.Constrain(s.tenant, s.kb, params, true)
}

// ProtectedDocumentTenant is for a driver's SQL owner join, after Guard.
func ProtectedDocumentTenant(ctx context.Context) (uint64, bool) {
	s, ok := ctx.Value(documentSearchContextKey{}).(documentSearchScope)
	return s.tenant, ok
}

type DocumentRetriever interface {
	EngineType() types.RetrieverEngineType
	Retrieve(context.Context, types.RetrieveParams) ([]*types.RetrieveResult, error)
}

// SearchDocuments runs the existing driver; no new search engine is created.
// The consumer is the model boundary (Rerank/LLM). Revalidation occurs before
// retrieval, before each consumer call and after it, before returning output.
// A concurrent withdrawal cancels output; bytes already sent to a model before
// the withdrawal cannot be retroactively recalled.
func SearchDocuments(ctx context.Context, grants []*DocumentReadGrant, engine DocumentRetriever, params types.RetrieveParams, consumers ...func(context.Context, []*types.IndexWithScore) error) ([]*types.IndexWithScore, error) {
	if engine == nil {
		return nil, ErrDocumentFilterDenied
	}
	ctx = WithDocumentSearch(ctx, grants...)
	constrained, err := GuardDocumentRetrieval(ctx, engine.EngineType(), params)
	if err != nil {
		return nil, err
	}
	rows, err := engine.Retrieve(ctx, constrained)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, id := range constrained.KnowledgeIDs {
		allowed[id] = true
	}
	out := []*types.IndexWithScore{}
	for _, group := range rows {
		if group == nil {
			return nil, ErrDocumentFilterDenied
		}
		for _, row := range group.Results {
			if row == nil || !allowed[row.KnowledgeID] || len(constrained.KnowledgeBaseIDs) != 1 || row.KnowledgeBaseID != constrained.KnowledgeBaseIDs[0] || !safeEnterpriseText(row.Content) {
				return nil, ErrDocumentFilterDenied
			}
			copy := *row
			out = append(out, &copy)
		}
	}
	check := func() error { _, e := GuardDocumentRetrieval(ctx, engine.EngineType(), constrained); return e }
	for _, consume := range consumers {
		if err := check(); err != nil {
			return nil, err
		}
		if consume == nil {
			return nil, ErrDocumentFilterDenied
		}
		if err := consume(ctx, out); err != nil {
			return nil, err
		}
	}
	if err := check(); err != nil {
		return nil, err
	}
	return out, nil
}

func safeEnterpriseText(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"http://", "https://", "resource://", "storage://", "backend://", "local://", "cos://", "s3://", "/files/", "/files?", "/r/", "!["} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}
