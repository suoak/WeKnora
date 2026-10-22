package access

import (
	"errors"
	"github.com/Tencent/WeKnora/internal/types"
)

var ErrDocumentFilterDenied = errors.New("document candidate scope denied")

// AuthorizedDocumentSet is an explicit allow-set. Its zero value is DENY_ALL,
// never the empty KnowledgeIDs convention used by legacy retrievers for ALL.
// It is a preparation API; no driver or existing pipeline is implicitly enabled.
type AuthorizedDocumentSet struct {
	tenantID uint64
	kbID     string
	ids      []string
}

func (g *EnterpriseReadGrant) DocumentSet() AuthorizedDocumentSet {
	if g == nil {
		return AuthorizedDocumentSet{}
	}
	return AuthorizedDocumentSet{tenantID: g.tenantID, kbID: g.kbID, ids: g.KnowledgeIDs()}
}

// Constrain must run before fetching candidates or calling external models.
// SupportsPreFilter must represent an actually validated driver/mode, not a
// request header. Unsupported filters block; they never fall back to ALL.
func (s AuthorizedDocumentSet) Constrain(tenantID uint64, kbID string, params types.RetrieveParams, supportsPreFilter bool) (types.RetrieveParams, error) {
	if !supportsPreFilter || s.tenantID == 0 || tenantID != s.tenantID || kbID != s.kbID || len(s.ids) == 0 || len(s.ids) > MaxDocumentAllowSet {
		return types.RetrieveParams{}, ErrDocumentFilterDenied
	}
	for _, id := range params.KnowledgeBaseIDs {
		if id != kbID {
			return types.RetrieveParams{}, ErrDocumentFilterDenied
		}
	}
	requested := map[string]bool{}
	for _, id := range params.KnowledgeIDs {
		requested[id] = true
	}
	ids := make([]string, 0, len(s.ids))
	for _, id := range s.ids {
		if len(requested) == 0 || requested[id] {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return types.RetrieveParams{}, ErrDocumentFilterDenied
	}
	params.KnowledgeBaseIDs = []string{kbID}
	params.KnowledgeIDs = ids
	return params, nil
}

// CheckDerivedSources denies unknown provenance and checks every source before
// a Wiki/FAQ/graph/summary/cache body can reach Rerank/LLM or a reader. It grants
// no access by itself. M3 consumers must supply current policy checks.
func CheckDerivedSources(sourceIDs []string, check func(string) error) error {
	if len(sourceIDs) == 0 || check == nil {
		return ErrDocumentFilterDenied
	}
	for _, id := range sourceIDs {
		if id == "" {
			return ErrDocumentFilterDenied
		}
		if err := check(id); err != nil {
			return err
		}
	}
	return nil
}
