package service

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"reflect"
	"sort"
	"strconv"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// A shared PostgreSQL engine has one lexical scoring corpus. Vector scores
// additionally require the exact same persisted embedding model identity.
// Never compare raw scores merely because two stores have the same driver.
type portalRankDomain struct {
	engine access.DocumentRetriever
	model  string
	kind   types.RetrieverType
	query  [32]byte
}

type portalRankCandidate struct {
	key, id   string
	score     float64
	localRank int
}

func portalCompatibleRankDomain(
	engine access.DocumentRetriever, model string, kind types.RetrieverType, embedding []float32,
) (portalRankDomain, bool) {
	if batch, ok := engine.(*portalBatchEngine); ok {
		engine = batch.engine
	}
	if engine == nil || engine.EngineType() != types.PostgresRetrieverEngineType ||
		!reflect.TypeOf(engine).Comparable() {
		return portalRankDomain{}, false
	}
	var query [32]byte
	if kind == types.KeywordsRetrieverType {
		model = ""
	} else if kind != types.VectorRetrieverType || model == "" || len(embedding) == 0 {
		return portalRankDomain{}, false
	} else {
		encoded := make([]byte, 4*len(embedding))
		for i, value := range embedding {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return portalRankDomain{}, false
			}
			binary.LittleEndian.PutUint32(encoded[4*i:], math.Float32bits(value))
		}
		query = sha256.Sum256(encoded)
	}
	return portalRankDomain{engine: engine, model: model, kind: kind, query: query}, true
}

// The per-KB authorized candidate sets remain unchanged. Reconstruct ranks
// within a genuinely comparable scoring domain before reciprocal-rank fusion,
// so an irrelevant first hit in a small KB does not get an artificial rank 1.
// This function grants no authority, hydrates no content and changes no failure
// accounting. The caller retains its canonical and final live output fences.
func portalApplyCompatibleRanks(
	hits map[string]*PortalMCPHit, outcomes []portalTargetSearchOutcome,
) error {
	domains := make(map[portalRankDomain]map[string]portalRankCandidate)
	subtract := make(map[string]float64)
	for _, outcome := range outcomes {
		for domain, candidates := range outcome.ranks {
			if domains[domain] == nil {
				domains[domain] = make(map[string]portalRankCandidate)
			}
			for key, candidate := range candidates {
				if hits[key] == nil || key != candidate.key || candidate.localRank < 1 ||
					math.IsNaN(candidate.score) || math.IsInf(candidate.score, 0) {
					return access.ErrForbidden
				}
				if old, exists := domains[domain][key]; exists && old != candidate {
					return access.ErrForbidden
				}
				if _, exists := domains[domain][key]; !exists {
					subtract[key] += 1 / float64(60+candidate.localRank)
				}
				domains[domain][key] = candidate
			}
		}
	}
	for key, contribution := range subtract {
		remaining := hits[key].RankScore - contribution
		if remaining < -1e-12 {
			return access.ErrForbidden
		}
		if math.Abs(remaining) < 1e-12 {
			remaining = 0
		}
		hits[key].RankScore = remaining
	}
	for _, candidates := range domains {
		ordered := make([]portalRankCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			ordered = append(ordered, candidate)
		}
		sort.Slice(ordered, func(i, j int) bool {
			a, b := ordered[i], ordered[j]
			if a.score != b.score {
				return a.score > b.score
			}
			ai, ae := strconv.ParseUint(a.id, 10, 64)
			bi, be := strconv.ParseUint(b.id, 10, 64)
			if ae == nil && be == nil && ai != bi {
				return ai < bi
			}
			return a.key < b.key
		})
		for rank, candidate := range ordered {
			hits[candidate.key].RankScore += 1 / float64(61+rank)
		}
	}
	return nil
}
