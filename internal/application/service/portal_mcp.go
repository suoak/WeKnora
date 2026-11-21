package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/perftrace"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// PortalMCPKnowledge is a bounded coordinator over existing access, repositories
// and retrieval drivers. It does not implement an index or issue memberships.
type PortalMCPKnowledge struct {
	personalRecovery   *PersonalRecovery
	Wiki               PersonalWikiReader
	Resolver           *access.PortalMCPResolver
	Knowledge          interfaces.KnowledgeRepository
	Chunks             interfaces.ChunkRepository
	Engine             func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error)
	Embed              func(context.Context, access.PortalKBDescriptor, string) ([]float32, error)
	SearchCapabilities func(context.Context, access.PortalKBDescriptor) ([]string, error)
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
	Results         []PortalMCPHit        `json:"results"`
	Searched        int                   `json:"searched_kbs"`
	Failed          int                   `json:"failed_kbs"`
	Status          string                `json:"status"`
	RequestedMode   string                `json:"requested_mode"`
	ResultCount     int                   `json:"result_count"`
	ModeFallbacks   []PortalMCPModeDetail `json:"mode_fallbacks"`
	PartialFailures []PortalMCPModeDetail `json:"partial_failures"`
	Skipped         int                   `json:"unsearchable_kbs"`
}

// PortalMCPModeDetail identifies an effective mode or a safe execution failure.
type PortalMCPModeDetail struct {
	KBID   string `json:"knowledge_base_id"`
	Mode   string `json:"mode"`
	Reason string `json:"reason"`
}

// Capabilities loads the target's real indexes through the bound application adapter.
func (s *PortalMCPKnowledge) Capabilities(ctx context.Context, d access.PortalKBDescriptor) ([]string, error) {
	if s.SearchCapabilities != nil {
		return s.SearchCapabilities(ctx, d)
	}
	return d.SearchCapabilities(), nil
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

func (s *PortalMCPKnowledge) document(
	ctx context.Context,
	t *access.PortalKBTarget,
	id string,
) (*types.Knowledge, error) {
	if s.Knowledge == nil || !portalContains(t, id) {
		return nil, access.ErrForbidden
	}
	d := t.Descriptor()
	read, err := s.publishedDocumentContext(ctx, id)
	if err != nil {
		return nil, err
	}
	row, err := s.Knowledge.GetKnowledgeByID(read, d.TenantID, id)
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
	if _, err := types.CheckExactPublishedDocumentRead(read); err != nil {
		return nil, err
	}
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
				if errors.Is(e, errPersonalExactDocument) {
					continue
				}
				return nil, e
			}
			out = append(out, portalHit(t, row, nil))
		}
	}
	if err = validatePortalTargets(ctx, targets); err != nil {
		return nil, err
	}
	if err = s.validatePublishedPortalHits(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func portalHit(t *access.PortalKBTarget, row *types.Knowledge, c *types.Chunk) PortalMCPHit {
	d := t.Descriptor()
	h := PortalMCPHit{
		TenantID:   d.TenantID,
		Space:      d.SpaceName,
		KBID:       d.ID,
		KBName:     d.Name,
		DocumentID: row.ID,
		Title:      row.Title,
		Reference:  fmt.Sprintf("knowhub:tenant:%d:kb:%s:document:%s", d.TenantID, d.ID, row.ID),
	}
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
		read, e := s.publishedDocumentContext(ctx, id)
		if e != nil {
			return nil, e
		}
		chunks, e := s.Chunks.ListChunksByKnowledgeID(read, t.Descriptor().TenantID, id)
		if e != nil {
			return nil, e
		}
		if len(chunks) > 1000 {
			return nil, access.ErrForbidden
		}
		out := []PortalMCPHit{}
		for _, c := range chunks {
			if c == nil || c.KnowledgeID != id || c.KnowledgeBaseID != row.KnowledgeBaseID ||
				c.TenantID != row.TenantID ||
				c.DeletedAt.Valid {
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
		if err = s.validatePublishedPortalHits(ctx, out); err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, access.ErrForbidden
}

// Keep the existing per-target credential, publication, canonical-body and source
// checks inside each task. Only independent KB tasks overlap; no authorization
// result is cached or shared between targets or requests.
const (
	portalSearchTargetConcurrency = 8
	portalSearchModeTimeout       = 10 * time.Second
	portalSearchFinalizeReserve   = 5 * time.Second
)

type portalTargetSearchOutcome struct {
	hits      map[string]*PortalMCPHit
	failed    bool
	partial   bool
	skipped   bool
	failures  []PortalMCPModeDetail
	fallbacks []PortalMCPModeDetail
}

func (s *PortalMCPKnowledge) searchPortalTarget(
	ctx, budget context.Context, t *access.PortalKBTarget, query, mode string, pattern *regexp.Regexp,
) (portalTargetSearchOutcome, error) {
	var outcome portalTargetSearchOutcome
	if budget.Err() != nil {
		outcome.failed = true
		outcome.failures = append(outcome.failures,
			PortalMCPModeDetail{t.Descriptor().ID, mode, "retrieval_budget_exhausted"})
		return outcome, nil
	}
	execution, e := t.SearchContext(ctx)
	if e != nil {
		return outcome, e
	}
	if deadline, ok := budget.Deadline(); ok {
		var cancel context.CancelFunc
		execution, cancel = context.WithDeadline(execution, deadline)
		defer cancel()
	}
	descriptor := t.Descriptor()
	caps, e := s.Capabilities(execution, descriptor)
	if e != nil {
		outcome.failed = true
		outcome.failures = append(
			outcome.failures,
			PortalMCPModeDetail{descriptor.ID, mode, "index_configuration_unavailable"},
		)
		return outcome, nil
	}
	hasKeyword, hasVector := false, false
	for _, capability := range caps {
		hasKeyword = hasKeyword || capability == "keyword"
		hasVector = hasVector || capability == "semantic"
	}
	if !hasKeyword && !hasVector || mode == "grep" && !hasKeyword {
		outcome.skipped = true
		return outcome, nil
	}
	types.ObserveMCPKnowledgeBase(ctx, descriptor.ID)
	engine, e := s.Engine(execution, descriptor)
	if e != nil || engine == nil || engine.EngineType() != types.PostgresRetrieverEngineType {
		outcome.failed = true
		outcome.failures = append(outcome.failures, PortalMCPModeDetail{descriptor.ID, mode, "retriever_unavailable"})
		return outcome, nil
	}
	kinds := []types.RetrieverType{types.KeywordsRetrieverType, types.VectorRetrieverType}
	if mode == "keyword" || mode == "grep" {
		kinds = kinds[:1]
	}
	if mode == "semantic" {
		kinds = kinds[1:]
	}
	if !hasKeyword {
		kinds = []types.RetrieverType{types.VectorRetrieverType}
	}
	if !hasVector {
		kinds = []types.RetrieverType{types.KeywordsRetrieverType}
	}
	local := map[string]*PortalMCPHit{}
	succeeded := []string{}
	for _, kind := range kinds {
		actualMode := "keyword"
		if kind == types.VectorRetrieverType {
			actualMode = "semantic"
		}
		modeFailed := func(reason string) {
			outcome.partial = true
			outcome.failures = append(outcome.failures, PortalMCPModeDetail{descriptor.ID, actualMode, reason})
		}
		if e = t.Revalidate(ctx); e != nil {
			return outcome, e
		}
		modeCtx, cancelMode := context.WithTimeout(execution, portalSearchModeTimeout)
		params := types.RetrieveParams{Query: query, RetrieverType: kind, TopK: 100, Threshold: -1}
		if kind == types.VectorRetrieverType {
			if s.Embed == nil {
				cancelMode()
				modeFailed("embedding_unavailable")
				continue
			}
			params.Embedding, e = s.Embed(modeCtx, descriptor, query)
			if e != nil {
				cancelMode()
				modeFailed("embedding_failed")
				continue
			}
		}
		params, e = access.GuardPortalRetrieval(execution, engine.EngineType(), params)
		if e != nil {
			cancelMode()
			return outcome, e
		}
		groups, partial, e := s.retrievePublishedPortalDocuments(modeCtx, t, engine, params)
		cancelMode()
		if partial {
			outcome.partial = true
		}
		if e != nil {
			modeFailed("retrieval_failed")
			continue
		}
		if e = t.Revalidate(ctx); e != nil {
			return outcome, e
		}
		rank := 0
		modeHits := map[string]*PortalMCPHit{}
		failed := false
		for _, g := range groups {
			if g == nil || g.Error != nil {
				failed = true
				break
			}
			for _, candidate := range g.Results {
				rank++
				if candidate == nil || !portalContains(t, candidate.KnowledgeID) ||
					candidate.KnowledgeBaseID != descriptor.ID ||
					candidate.SourceType != types.ChunkSourceType {
					return outcome, access.ErrForbidden
				}
				if mode != "grep" {
					// Rank the complete authorized index candidate set first.
					// Only final hits need canonical body/title reads; source and
					// credential revalidation still precede every output boundary.
					hit := portalHit(t, &types.Knowledge{ID: candidate.KnowledgeID},
						&types.Chunk{ID: candidate.ChunkID, Content: candidate.Content})
					key := hit.Reference + ":" + candidate.ChunkID
					if modeHits[key] == nil {
						modeHits[key] = &hit
					} else if modeHits[key].Text != hit.Text {
						return outcome, access.ErrForbidden
					} else {
						continue // use the best rank once per mode
					}
					modeHits[key].RankScore += 1 / float64(60+rank)
					continue
				}
				// Completeness/canonical equality is enforced in SQL before candidates.
				// Re-read canonical title/content after obtaining the live target lease.
				row, e := s.document(ctx, t, candidate.KnowledgeID)
				if e != nil {
					return outcome, e
				}
				read, e := s.publishedDocumentContext(ctx, candidate.KnowledgeID)
				if e != nil {
					return outcome, e
				}
				chunk, e := s.Chunks.GetChunkByID(read, descriptor.TenantID, candidate.ChunkID)
				if e != nil {
					return outcome, e
				}
				if chunk == nil || chunk.KnowledgeID != row.ID || chunk.KnowledgeBaseID != descriptor.ID ||
					chunk.TenantID != descriptor.TenantID ||
					chunk.DeletedAt.Valid ||
					!chunk.IsEnabled ||
					chunk.ChunkType != types.ChunkTypeText ||
					chunk.Content != candidate.Content {
					return outcome, access.ErrForbidden
				}
				text, e := access.SanitizePortalText(chunk.Content)
				if e != nil {
					return outcome, e
				}
				if mode == "grep" && !pattern.MatchString(text) {
					continue
				}
				canonical := *chunk
				canonical.Content = text
				hit := portalHit(t, row, &canonical)
				key := hit.Reference + ":" + chunk.ID
				if modeHits[key] == nil {
					modeHits[key] = &hit
				} else {
					continue
				}
				modeHits[key].RankScore += 1 / float64(60+rank)
			}
		}
		if failed {
			modeFailed("retrieval_failed")
			continue
		}
		succeeded = append(succeeded, actualMode)
		for key, hit := range modeHits {
			if local[key] == nil {
				local[key] = hit
			} else {
				if local[key].Text != hit.Text {
					return outcome, access.ErrForbidden
				}
				local[key].RankScore += hit.RankScore
			}
		}
	}
	if len(succeeded) == 0 {
		outcome.failed = true
		return outcome, nil
	}
	if mode != "grep" && (len(succeeded) == 1 && mode != succeeded[0]) {
		reason := "index_unavailable"
		if len(outcome.failures) > 0 {
			reason = "mode_failed"
		}
		outcome.fallbacks = append(outcome.fallbacks, PortalMCPModeDetail{descriptor.ID, succeeded[0], reason})
	}
	outcome.hits = local
	return outcome, nil
}

// Search uses per-target/per-mode ranks (RRF), never compares raw cosine/BM25
// scores across different indexes or embeddings. Failed targets are explicit.
func (s *PortalMCPKnowledge) Search(
	ctx context.Context,
	ids []string,
	query, mode string,
	limit int,
) (*PortalMCPSearchResult, error) {
	return s.search(ctx, ids, query, mode, limit, nil)
}

// SearchPattern uses index terms separately from the regular expression, as
// the built-in MCP does. Every returned chunk still has to match the pattern.
func (s *PortalMCPKnowledge) SearchPattern(
	ctx context.Context,
	ids []string,
	terms string,
	pattern *regexp.Regexp,
	limit int,
) (*PortalMCPSearchResult, error) {
	if pattern == nil {
		return nil, access.ErrForbidden
	}
	return s.search(ctx, ids, terms, "grep", limit, pattern)
}

func (s *PortalMCPKnowledge) search(
	ctx context.Context,
	ids []string,
	query, mode string,
	limit int,
	pattern *regexp.Regexp,
) (*PortalMCPSearchResult, error) {
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
	if cleanErr := access.ValidatePortalQuery(query); cleanErr != nil {
		return nil, access.ErrForbidden
	}
	var err error
	if mode == "grep" && pattern == nil {
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
	ctx = withPortalQueryEmbeddings(ctx)
	// Leave time for the existing live authorization and canonical output
	// fences. A retrieval deadline must not erase already successful KBs.
	budget := ctx
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		reserve := min(portalSearchFinalizeReserve, time.Until(deadline)/5)
		budget, cancel = context.WithDeadline(ctx, deadline.Add(-reserve))
		defer cancel()
	}
	result := &PortalMCPSearchResult{
		Results:         []PortalMCPHit{},
		Status:          "complete",
		RequestedMode:   mode,
		ModeFallbacks:   []PortalMCPModeDetail{},
		PartialFailures: []PortalMCPModeDetail{},
	}
	if len(targets) == 0 {
		result.Status = "no_scope"
	}
	aggregate := map[string]*PortalMCPHit{}
	byKB := map[string]*access.PortalKBTarget{}
	// Bound fan-out within the existing request admission limits and deadline.
	// Each worker owns one target and result slot. Merge only after every worker
	// exits, so authorization failures discard all in-flight results.
	outcomes := make([]portalTargetSearchOutcome, len(targets))
	for i, target := range targets {
		outcomes[i] = portalTargetSearchOutcome{failed: true, failures: []PortalMCPModeDetail{
			{target.Descriptor().ID, mode, "retrieval_budget_exhausted"},
		}}
	}
	group, execution := errgroup.WithContext(ctx)
	group.SetLimit(portalSearchTargetConcurrency)
	for _, i := range portalSearchOrder(ctx, s.Resolver.Store, targets) {
		target := targets[i]
		if execution.Err() != nil || budget.Err() != nil {
			break
		}
		group.Go(func() error {
			var targetErr error
			outcomes[i], targetErr = s.searchPortalTarget(execution, budget, target, query, mode, pattern)
			return targetErr
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i, outcome := range outcomes {
		result.ModeFallbacks = append(result.ModeFallbacks, outcome.fallbacks...)
		result.PartialFailures = append(result.PartialFailures, outcome.failures...)
		if outcome.skipped {
			result.Skipped++
			continue
		}
		byKB[targets[i].Descriptor().ID] = targets[i]
		if outcome.partial {
			result.Status = "partial"
		}
		if outcome.failed {
			result.Failed++
			continue
		}
		result.Searched++
		for key, hit := range outcome.hits {
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
	if mode != "grep" {
		for i, ranked := range result.Results {
			t := byKB[ranked.KBID]
			if t == nil {
				return nil, access.ErrForbidden
			}
			row, e := s.document(ctx, t, ranked.DocumentID)
			if e != nil {
				return nil, e
			}
			read, e := s.publishedDocumentContext(ctx, ranked.DocumentID)
			if e != nil {
				return nil, e
			}
			chunk, e := s.Chunks.GetChunkByID(read, ranked.TenantID, ranked.ChunkID)
			if e != nil {
				return nil, e
			}
			if chunk == nil || chunk.KnowledgeID != row.ID || chunk.KnowledgeBaseID != ranked.KBID ||
				chunk.TenantID != ranked.TenantID || chunk.DeletedAt.Valid || !chunk.IsEnabled ||
				chunk.ChunkType != types.ChunkTypeText || chunk.Content != ranked.Text {
				return nil, access.ErrForbidden
			}
			text, e := access.SanitizePortalText(chunk.Content)
			if e != nil {
				return nil, e
			}
			canonical := *chunk
			canonical.Content = text
			hit := portalHit(t, row, &canonical)
			hit.RankScore = ranked.RankScore
			result.Results[i] = hit
		}
	}
	if diagnostics.Partial() {
		result.Status = "partial"
	}
	if result.Failed > 0 {
		result.Status = "partial"
		if result.Searched == 0 {
			result.Status = "failed"
		}
	}
	if len(targets) > 0 && result.Skipped == len(targets) {
		result.Status = "no_searchable_index"
	}
	result.ResultCount = len(result.Results)
	for _, consumer := range s.Consumers {
		if e := s.validatePublishedPortalHits(ctx, result.Results); e != nil {
			return nil, e
		}
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
	if e := s.validatePublishedPortalHits(ctx, result.Results); e != nil {
		return nil, e
	}
	return result, nil
}
