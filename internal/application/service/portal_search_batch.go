package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

type portalBatchEnginesKey struct{}

type portalBatchIdentity struct {
	engine       access.DocumentRetriever
	tenant       uint64
	model, modes string
	public       bool
}

type portalBatchReply struct {
	done    chan struct{}
	results map[string][]*types.RetrieveResult
	errors  map[string]error
}

// A request-local adapter over the existing retriever. The coordinator still
// ranks and validates every KB independently; only identical SQL work is merged.
type portalBatchEngine struct {
	engine  access.DocumentRetriever
	targets []*access.PortalKBTarget
	mu      sync.Mutex
	calls   map[string]*portalBatchReply
	// Keyword batches may share a scan across public tenants. Vector batches
	// keep their original tenant/model/store identity and scheduling budget.
	keywords    map[string]*portalBatchEngine
	keywordOnly bool
}

func (s *PortalMCPKnowledge) portalBatchContext(ctx context.Context, targets []*access.PortalKBTarget) context.Context {
	if !s.Resolver.PublishedDefaults || !ordinaryLocalSourceRead(ctx) && s.personalRecovery != nil {
		return ctx
	}
	buckets := make(map[portalBatchIdentity][]*access.PortalKBTarget)
	keywordBuckets := make(map[portalBatchIdentity][]*access.PortalKBTarget)
	for _, target := range targets {
		d := target.Descriptor()
		caps, err := s.Capabilities(ctx, d)
		if err != nil {
			continue
		}
		engine, err := s.Engine(ctx, d)
		if err != nil || engine == nil || engine.EngineType() != types.PostgresRetrieverEngineType ||
			!reflect.TypeOf(engine).Comparable() {
			continue
		}
		key := portalBatchIdentity{engine, d.TenantID, d.EmbeddingModelID, strings.Join(caps, ","), target.Public()}
		buckets[key] = append(buckets[key], target)
		if target.Public() && strings.Contains(","+key.modes+",", ",keyword,") {
			key.tenant = 0
			keywordBuckets[key] = append(keywordBuckets[key], target)
		}
	}
	keywords := portalKeywordBatches(keywordBuckets)
	engines := make(map[string]access.DocumentRetriever)
	for key, bucket := range buckets {
		// Bound materialized lexical work without selecting or dropping KBs.
		// Unlike a global TopK, each batch retains 100 candidates per KB/mode.
		for start := 0; start < len(bucket); start += 8 {
			part := bucket[start:min(start+8, len(bucket))]
			batch := &portalBatchEngine{
				engine: key.engine, targets: part, calls: make(map[string]*portalBatchReply), keywords: keywords,
			}
			for _, target := range part {
				engines[target.Descriptor().ID] = batch
			}
		}
	}
	return context.WithValue(ctx, portalBatchEnginesKey{}, engines)
}

func portalKeywordBatches(buckets map[portalBatchIdentity][]*access.PortalKBTarget) map[string]*portalBatchEngine {
	engines := make(map[string]*portalBatchEngine)
	for key, targets := range buckets {
		weights := make(map[string]int, len(targets))
		for _, target := range targets {
			weights[target.Descriptor().ID] = len(target.DocumentIDs())
		}
		sort.Slice(targets, func(i, j int) bool {
			a, b := targets[i].Descriptor().ID, targets[j].Descriptor().ID
			if weights[a] != weights[b] {
				return weights[a] > weights[b]
			}
			return a < b
		})
		for start := 0; start < len(targets); {
			end, weight := start, 0
			for end < len(targets) && end-start < 16 {
				next := weights[targets[end].Descriptor().ID]
				if end > start && weight+next > 1024 {
					break
				}
				weight += next
				end++
			}
			batch := &portalBatchEngine{
				engine: key.engine, targets: targets[start:end],
				calls: make(map[string]*portalBatchReply), keywordOnly: true,
			}
			for _, target := range batch.targets {
				engines[target.Descriptor().ID] = batch
			}
			start = end
		}
	}
	return engines
}

func portalBatchConcurrency(ctx context.Context) int {
	engines, _ := ctx.Value(portalBatchEnginesKey{}).(map[string]access.DocumentRetriever)
	if len(engines) > 0 {
		// Reuse the existing Store fan-out budget. Sixteen admitted MCP calls
		// must not each open eight expensive PostgreSQL tasks against a 100-
		// connection server. This leaves the user/global admission pools intact.
		return defaultMultiStoreFanoutLimit
	}
	return portalSearchTargetConcurrency
}

func portalBatchWork(ctx context.Context, targets []*access.PortalKBTarget, order []int) [][]int {
	engines, _ := ctx.Value(portalBatchEnginesKey{}).(map[string]access.DocumentRetriever)
	if len(engines) == 0 {
		work := make([][]int, 0, len(order))
		for _, i := range order {
			work = append(work, []int{i})
		}
		return work
	}
	var buckets [][]int
	positions := make(map[access.DocumentRetriever]int)
	for _, i := range order {
		engine := engines[targets[i].Descriptor().ID]
		if engine == nil {
			buckets = append(buckets, []int{i})
			continue
		}
		position, found := positions[engine]
		if !found {
			position = len(buckets)
			positions[engine] = position
			buckets = append(buckets, nil)
		}
		buckets[position] = append(buckets[position], i)
	}
	// One worker executes a batch and immediately consumes its per-KB results.
	// Queuing cached followers behind every other SQL batch can exhaust their
	// budget after the database has already successfully searched those KBs.
	return buckets
}

func (s *PortalMCPKnowledge) portalSearchEngine(
	ctx context.Context, d access.PortalKBDescriptor,
) (access.DocumentRetriever, error) {
	if engines, _ := ctx.Value(portalBatchEnginesKey{}).(map[string]access.DocumentRetriever); engines[d.ID] != nil {
		return engines[d.ID], nil
	}
	return s.Engine(ctx, d)
}

func (b *portalBatchEngine) EngineType() types.RetrieverEngineType { return b.engine.EngineType() }

func (b *portalBatchEngine) Retrieve(
	ctx context.Context, params types.RetrieveParams,
) ([]*types.RetrieveResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(params.KnowledgeBaseIDs) != 1 || len(params.KnowledgeIDs) > 0 {
		return b.engine.Retrieve(ctx, params)
	}
	kb := params.KnowledgeBaseIDs[0]
	member := false
	for _, target := range b.targets {
		member = member || target.Descriptor().ID == kb
	}
	if !member {
		return nil, access.ErrForbidden
	}
	if params.RetrieverType == types.KeywordsRetrieverType && b.keywords[kb] != nil {
		return b.keywords[kb].Retrieve(ctx, params)
	}
	identity, err := json.Marshal(struct {
		Query     string
		Kind      types.RetrieverType
		Embedding []float32
		TopK      int
		Threshold float64
	}{params.Query, params.RetrieverType, params.Embedding, params.TopK, params.Threshold})
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%x", sha256.Sum256(identity))
	b.mu.Lock()
	call, found := b.calls[key]
	if !found {
		call = &portalBatchReply{
			done:    make(chan struct{}),
			results: make(map[string][]*types.RetrieveResult),
			errors:  make(map[string]error),
		}
		b.calls[key] = call
	}
	b.mu.Unlock()
	if found {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
		}
	} else {
		b.retrieve(ctx, params, call)
		close(call.done)
	}
	if err := call.errors[kb]; err != nil {
		return nil, err
	}
	return clonePortalBatchResults(call.results[kb]), nil
}

func (b *portalBatchEngine) retrieve(ctx context.Context, params types.RetrieveParams, call *portalBatchReply) {
	authorize := access.PortalGroupSearchContext
	if b.keywordOnly {
		authorize = access.PortalKeywordGroupSearchContext
	}
	group, err := authorize(ctx, b.targets)
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			err = canceled
		}
		for _, target := range b.targets {
			call.errors[target.Descriptor().ID] = err
		}
		return
	}
	params.KnowledgeBaseIDs = nil
	byKB := make(map[string]*access.PortalKBTarget)
	for _, target := range b.targets {
		id := target.Descriptor().ID
		params.KnowledgeBaseIDs = append(params.KnowledgeBaseIDs, id)
		byKB[id] = target
	}
	rows, err := b.engine.Retrieve(group, params)
	for _, row := range rows {
		if row != nil && row.Error != nil {
			err = row.Error
		}
	}
	if err != nil {
		if canceled := ctx.Err(); canceled != nil {
			for id := range byKB {
				call.errors[id] = canceled
			}
			return
		}
		// A corrupt/incompatible KB must not erase another KB's successful leg.
		// Split only on a real execution failure, inside the existing mode budget.
		for _, target := range b.targets {
			id := target.Descriptor().ID
			execution, e := target.SearchContext(ctx)
			if e == nil {
				narrow := params
				narrow.KnowledgeBaseIDs = []string{id}
				call.results[id], e = b.engine.Retrieve(execution, narrow)
			}
			if canceled := ctx.Err(); canceled != nil {
				e = canceled
			}
			call.errors[id] = e
		}
		return
	}
	for _, row := range rows {
		if row == nil {
			continue
		}
		for _, candidate := range row.Results {
			if candidate == nil || byKB[candidate.KnowledgeBaseID] == nil ||
				!portalContains(byKB[candidate.KnowledgeBaseID], candidate.KnowledgeID) {
				for id := range byKB {
					call.errors[id] = access.ErrForbidden
				}
				return
			}
		}
		for id := range byKB {
			part := *row
			part.Results = nil
			for _, candidate := range row.Results {
				if candidate.KnowledgeBaseID == id {
					part.Results = append(part.Results, candidate)
				}
			}
			call.results[id] = append(call.results[id], &part)
		}
	}
}

func clonePortalBatchResults(rows []*types.RetrieveResult) []*types.RetrieveResult {
	var out []*types.RetrieveResult
	for _, row := range rows {
		if row == nil {
			continue
		}
		cloned := *row
		cloned.Results = nil
		for _, candidate := range row.Results {
			if candidate == nil {
				cloned.Results = append(cloned.Results, nil)
				continue
			}
			value := *candidate
			cloned.Results = append(cloned.Results, &value)
		}
		out = append(out, &cloned)
	}
	return out
}

func portalBatchAuthorizationError(err error) bool {
	return errors.Is(err, access.ErrForbidden) || errors.Is(err, access.ErrUnauthorized) ||
		errors.Is(err, access.ErrDocumentFilterDenied)
}
