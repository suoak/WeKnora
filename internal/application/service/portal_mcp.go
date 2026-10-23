package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/perftrace"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// PortalMCPKnowledge is a bounded coordinator over existing access, repositories
// and retrieval drivers. It does not implement an index or issue memberships.
type PortalMCPKnowledge struct {
	Wiki      PersonalWikiReader
	Resolver  *access.PortalMCPResolver
	Knowledge interfaces.KnowledgeRepository
	Chunks    interfaces.ChunkRepository
	Engine    func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error)
	Embed     func(context.Context, access.PortalKBDescriptor, string) ([]float32, error)
	// Consumers are trusted model adapters (Rerank/LLM), not request parameters.
	Consumers []func(context.Context, []PortalMCPHit) error
}
type PortalMCPHit struct {
	TenantID   uint64  `json:"tenant_id"`
	Space      string  `json:"space"`
	KBID       string  `json:"knowledge_base_id"`
	KBName     string  `json:"knowledge_base_name"`
	DocumentID string  `json:"knowledge_id"`
	ChunkID    string  `json:"chunk_id"`
	Title      string  `json:"title"`
	Text       string  `json:"content"`
	Reference  string  `json:"reference"`
	RankScore  float64 `json:"rank_score"`
}
type PortalMCPSearchResult struct {
	Results  []PortalMCPHit `json:"results"`
	Searched int            `json:"searched_kbs"`
	Failed   int            `json:"failed_kbs"`
	Status   string         `json:"status"`
}

func (s *PortalMCPKnowledge) Resolve(ctx context.Context, ids []string) ([]*access.PortalKBTarget, error) {
	if s == nil || s.Resolver == nil {
		return nil, access.ErrForbidden
	}
	return s.Resolver.Resolve(ctx, ids)
}
func validatePortalTargets(ctx context.Context, ts []*access.PortalKBTarget) error {
	for _, t := range ts {
		if err := t.Revalidate(ctx); err != nil {
			return err
		}
	}
	return nil
}
func portalContains(t *access.PortalKBTarget, id string) bool {
	for _, d := range t.DocumentIDs() {
		if d == id {
			return true
		}
	}
	return false
}
func (s *PortalMCPKnowledge) document(ctx context.Context, t *access.PortalKBTarget, id string) (*types.Knowledge, error) {
	if s.Knowledge == nil || !portalContains(t, id) {
		return nil, access.ErrForbidden
	}
	d := t.Descriptor()
	row, err := s.Knowledge.GetKnowledgeByID(ctx, d.TenantID, id)
	if err != nil {
		return nil, err
	}
	if row == nil || row.KnowledgeBaseID != d.ID || row.TenantID != d.TenantID {
		return nil, access.ErrForbidden
	}
	title, err := access.SanitizePortalText(row.Title)
	if err != nil {
		return nil, err
	}
	copy := *row
	copy.Title = title
	return &copy, nil
}

// ListDocuments emits only certified metadata and never repository structs.
func (s *PortalMCPKnowledge) ListDocuments(ctx context.Context, kb string) ([]PortalMCPHit, error) {
	targets, err := s.Resolve(ctx, []string{kb})
	if err != nil {
		return nil, err
	}
	out := []PortalMCPHit{}
	for _, t := range targets {
		for _, id := range t.DocumentIDs() {
			row, e := s.document(ctx, t, id)
			if e != nil {
				return nil, e
			}
			out = append(out, portalHit(t, row, nil))
		}
	}
	if err = validatePortalTargets(ctx, targets); err != nil {
		return nil, err
	}
	return out, nil
}
func portalHit(t *access.PortalKBTarget, row *types.Knowledge, c *types.Chunk) PortalMCPHit {
	d := t.Descriptor()
	h := PortalMCPHit{TenantID: d.TenantID, Space: d.SpaceName, KBID: d.ID, KBName: d.Name, DocumentID: row.ID, Title: row.Title, Reference: fmt.Sprintf("knowhub:tenant:%d:kb:%s:document:%s", d.TenantID, d.ID, row.ID)}
	if c != nil {
		h.ChunkID = c.ID
		h.Text = c.Content
	}
	return h
}
func (s *PortalMCPKnowledge) ReadDocument(ctx context.Context, id string) ([]PortalMCPHit, error) {
	targets, err := s.Resolve(ctx, nil)
	if err != nil {
		return nil, err
	}
	for _, t := range targets {
		if !portalContains(t, id) {
			continue
		}
		row, e := s.document(ctx, t, id)
		if e != nil {
			return nil, e
		}
		if s.Chunks == nil {
			return nil, access.ErrForbidden
		}
		chunks, e := s.Chunks.ListChunksByKnowledgeID(ctx, t.Descriptor().TenantID, id)
		if e != nil {
			return nil, e
		}
		if len(chunks) > 1000 {
			return nil, access.ErrForbidden
		}
		out := []PortalMCPHit{}
		for _, c := range chunks {
			if c == nil || c.KnowledgeID != id || c.KnowledgeBaseID != row.KnowledgeBaseID || c.TenantID != row.TenantID || c.DeletedAt.Valid {
				return nil, access.ErrForbidden
			}
			if !c.IsEnabled {
				continue
			}
			if c.ChunkType != types.ChunkTypeText {
				return nil, access.ErrForbidden
			}
			text, err := access.SanitizePortalText(c.Content)
			if err != nil {
				return nil, err
			}
			copy := *c
			copy.Content = text
			out = append(out, portalHit(t, row, &copy))
		}
		if err = t.Revalidate(ctx); err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, access.ErrForbidden
}

// Search uses per-target/per-mode ranks (RRF), never compares raw cosine/BM25
// scores across different indexes or embeddings. Failed targets are explicit.
func (s *PortalMCPKnowledge) Search(ctx context.Context, ids []string, query, mode string, limit int) (*PortalMCPSearchResult, error) {
	if query == "" || len(query) > 4096 || limit < 1 || limit > 30 || s.Engine == nil {
		return nil, access.ErrForbidden
	}
	if mode == "" {
		mode = "hybrid"
	}
	if mode != "hybrid" && mode != "keyword" && mode != "semantic" && mode != "grep" {
		return nil, access.ErrForbidden
	}
	// User-supplied destinations are not forwarded to external embedders either.
	query, cleanErr := access.SanitizePortalText(query)
	if cleanErr != nil || query == "" {
		return nil, access.ErrForbidden
	}
	var pattern *regexp.Regexp
	var err error
	if mode == "grep" {
		pattern, err = regexp.Compile("(?i)" + query)
		if err != nil {
			return nil, access.ErrForbidden
		}
	}
	ctx, diagnostics := access.WithPortalDiagnostics(ctx)
	targets, err := s.Resolve(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := &PortalMCPSearchResult{Results: []PortalMCPHit{}, Status: "complete"}
	aggregate := map[string]*PortalMCPHit{}
	for _, t := range targets {
		execution, e := t.SearchContext(ctx)
		if e != nil {
			return nil, e
		}
		descriptor := t.Descriptor()
		engine, e := s.Engine(execution, descriptor)
		if e != nil || engine == nil || engine.EngineType() != types.PostgresRetrieverEngineType {
			result.Failed++
			continue
		}
		kinds := []types.RetrieverType{types.KeywordsRetrieverType, types.VectorRetrieverType}
		if mode == "keyword" || mode == "grep" {
			kinds = kinds[:1]
		}
		if mode == "semantic" {
			kinds = kinds[1:]
		}
		local := map[string]*PortalMCPHit{}
		failed := false
		for _, kind := range kinds {
			if e = t.Revalidate(ctx); e != nil {
				return nil, e
			}
			params := types.RetrieveParams{Query: query, RetrieverType: kind, TopK: 100, Threshold: -1}
			if kind == types.VectorRetrieverType {
				if s.Embed == nil {
					failed = true
					break
				}
				params.Embedding, e = s.Embed(execution, descriptor, query)
				if e != nil {
					failed = true
					break
				}
			}
			params, e = access.GuardPortalRetrieval(execution, engine.EngineType(), params)
			if e != nil {
				return nil, e
			}
			groups, e := engine.Retrieve(execution, params)
			if e != nil {
				failed = true
				break
			}
			if e = t.Revalidate(ctx); e != nil {
				return nil, e
			}
			rank := 0
			for _, g := range groups {
				if g == nil || g.Error != nil {
					failed = true
					break
				}
				for _, candidate := range g.Results {
					rank++
					if candidate == nil || !portalContains(t, candidate.KnowledgeID) || candidate.KnowledgeBaseID != descriptor.ID || candidate.SourceType != types.ChunkSourceType {
						return nil, access.ErrForbidden
					}
					// Completeness/canonical equality is enforced in SQL before candidates.
					// Re-read canonical title/content after obtaining the live target lease.
					row, e := s.document(ctx, t, candidate.KnowledgeID)
					if e != nil {
						return nil, e
					}
					chunk, e := s.Chunks.GetChunkByID(ctx, descriptor.TenantID, candidate.ChunkID)
					if e != nil {
						return nil, e
					}
					if chunk == nil || chunk.KnowledgeID != row.ID || chunk.KnowledgeBaseID != descriptor.ID || chunk.TenantID != descriptor.TenantID || chunk.DeletedAt.Valid || !chunk.IsEnabled || chunk.ChunkType != types.ChunkTypeText || chunk.Content != candidate.Content {
						return nil, access.ErrForbidden
					}
					text, e := access.SanitizePortalText(chunk.Content)
					if e != nil {
						return nil, e
					}
					if mode == "grep" && !pattern.MatchString(text) {
						continue
					}
					copy := *chunk
					copy.Content = text
					hit := portalHit(t, row, &copy)
					key := hit.Reference + ":" + chunk.ID
					if local[key] == nil {
						local[key] = &hit
					}
					local[key].RankScore += 1 / float64(60+rank)
				}
			}
			if failed {
				break
			}
		}
		if failed {
			result.Failed++
			continue
		}
		result.Searched++
		for key, hit := range local {
			aggregate[key] = hit
		}
	}
	mergeDone := perftrace.Measure(ctx, perftrace.Merge)
	for _, hit := range aggregate {
		result.Results = append(result.Results, *hit)
	}
	sort.Slice(result.Results, func(i, j int) bool {
		a, b := result.Results[i], result.Results[j]
		if a.RankScore == b.RankScore {
			return a.Reference+a.ChunkID < b.Reference+b.ChunkID
		}
		return a.RankScore > b.RankScore
	})
	if len(result.Results) > limit {
		result.Results = result.Results[:limit]
	}
	mergeDone()
	if diagnostics.Partial() {
		result.Status = "partial"
	}
	if result.Failed > 0 {
		result.Status = "partial"
		if result.Searched == 0 {
			result.Status = "failed"
		}
	}
	for _, consumer := range s.Consumers {
		if e := validatePortalTargets(ctx, targets); e != nil {
			return nil, e
		}
		if consumer == nil {
			return nil, access.ErrForbidden
		}
		if e := consumer(ctx, append([]PortalMCPHit(nil), result.Results...)); e != nil {
			return nil, e
		}
	}
	if e := validatePortalTargets(ctx, targets); e != nil {
		return nil, e
	}
	return result, nil
}
