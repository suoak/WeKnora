package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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
}

func (s *PortalMCPKnowledge) portalBatchContext(ctx context.Context, targets []*access.PortalKBTarget) context.Context {
	if !s.Resolver.PublishedDefaults || !ordinaryLocalSourceRead(ctx) && s.personalRecovery != nil {
		return ctx
	}
	buckets := make(map[portalBatchIdentity][]*access.PortalKBTarget)
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
	}
	engines := make(map[string]access.DocumentRetriever)
	for key, bucket := range buckets {
		// Bound materialized lexical work without selecting or dropping KBs.
		// Unlike a global TopK, each batch retains 100 candidates per KB/mode.
		for start := 0; start < len(bucket); start += 8 {
			part := bucket[start:min(start+8, len(bucket))]
			batch := &portalBatchEngine{engine: key.engine, targets: part, calls: make(map[string]*portalBatchReply)}
			for _, target := range part {
				engines[target.Descriptor().ID] = batch
			}
		}
	}
	return context.WithValue(ctx, portalBatchEnginesKey{}, engines)
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

func portalBatchOrder(ctx context.Context, targets []*access.PortalKBTarget, order []int) []int {
	engines, _ := ctx.Value(portalBatchEnginesKey{}).(map[string]access.DocumentRetriever)
	if len(engines) == 0 {
		return order
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
	result := make([]int, 0, len(order))
	for round := 0; len(result) < len(order); round++ {
		for _, bucket := range buckets {
			if round < len(bucket) {
				result = append(result, bucket[round])
			}
		}
	}
	return result
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
	if len(params.KnowledgeBaseIDs) != 1 || len(params.KnowledgeIDs) > 0 {
		return b.engine.Retrieve(ctx, params)
	}
	kb := params.KnowledgeBaseIDs[0]
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
	group, err := access.PortalGroupSearchContext(ctx, b.targets)
	if err != nil {
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
		copy := *row
		copy.Results = nil
		for _, candidate := range row.Results {
			if candidate == nil {
				copy.Results = append(copy.Results, nil)
				continue
			}
			value := *candidate
			copy.Results = append(copy.Results, &value)
		}
		out = append(out, &copy)
	}
	return out
}

func portalBatchAuthorizationError(err error) bool {
	return errors.Is(err, access.ErrForbidden) || errors.Is(err, access.ErrUnauthorized) ||
		errors.Is(err, access.ErrDocumentFilterDenied)
}
