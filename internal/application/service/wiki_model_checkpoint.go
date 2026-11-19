package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

const (
	wikiModelCheckpointTTL        = 2 * time.Hour
	wikiModelCheckpointMaxBytes   = 64 << 10
	wikiModelCheckpointMaxEntries = 128
)

type wikiModelCheckpoint struct {
	key, field, purpose string
	validate            func(context.Context) bool
}

func (s *wikiIngestService) wikiModelCheckpoint(
	ctx context.Context, modelID, purpose, requestKey string,
) *wikiModelCheckpoint {
	if s.redisClient == nil || s.modelService == nil || ctx.Err() != nil {
		return nil
	}
	// Cache only source-derived map stages. Page rewrites, directory changes
	// and other operations still run against their fresh page dependencies.
	switch purpose {
	case "wiki_summary", "wiki_knowledge_extract", "wiki_chunk_citation":
	default:
		return nil
	}
	tenant, kb, inputs, ok := repository.WikiModelCheckpointScope(ctx)
	if !ok {
		return nil
	}
	currentTenant, scoped := types.TenantIDFromContext(ctx)
	if !scoped || currentTenant != tenant {
		return nil
	}
	pages, ok := s.wikiService.(*wikiPageService)
	if !ok {
		return nil
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	model, err := s.modelService.GetModelByID(bounded, modelID)
	if err != nil || model == nil || model.ID != modelID || model.DeletedAt.Valid {
		return nil
	}
	// Hash the actual resolved row, including parameter and update changes.
	// Credentials never appear in Redis keys, values or diagnostic messages.
	config, err := json.Marshal(model)
	if err != nil {
		return nil
	}
	b, err := json.Marshal([]any{"v1", inputs, modelID, config, purpose, requestKey})
	if err != nil {
		return nil
	}
	digest := sha256.Sum256(b)
	return &wikiModelCheckpoint{
		key:   fmt.Sprintf("wiki:model-checkpoint:v1:{%d:%s}", tenant, kb),
		field: hex.EncodeToString(digest[:]), purpose: purpose,
		validate: func(fresh context.Context) bool {
			return repository.WikiModelCheckpointReady(fresh, pages.repo, kb, inputs)
		},
	}
}

func validWikiModelCheckpoint(purpose string, result wikiTemplateResult) bool {
	if strings.TrimSpace(result.Content) == "" || len(result.Content) > wikiModelCheckpointMaxBytes ||
		isLengthStopFinishReason(result.FinishReason) {
		return false
	}
	switch purpose {
	case "wiki_summary":
		return true
	case "wiki_chunk_citation":
		var parsed citationBatchResult
		return json.Unmarshal([]byte(cleanLLMJSON(result.Content)), &parsed) == nil && parsed.Citations != nil
	case "wiki_knowledge_extract":
		var parsed combinedExtraction
		return json.Unmarshal([]byte(cleanLLMJSON(result.Content)), &parsed) == nil &&
			(len(parsed.Entities) > 0 || len(parsed.Concepts) > 0)
	default:
		return false
	}
}

func (s *wikiIngestService) loadWikiModelCheckpoint(
	ctx context.Context, checkpoint *wikiModelCheckpoint,
) (wikiTemplateResult, bool) {
	if checkpoint == nil || ctx.Err() != nil {
		return wikiTemplateResult{}, false
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	data, err := s.redisClient.HGet(bounded, checkpoint.key, checkpoint.field).Bytes()
	var result wikiTemplateResult
	if err != nil || len(data) > wikiModelCheckpointMaxBytes+1024 || json.Unmarshal(data, &result) != nil ||
		!validWikiModelCheckpoint(checkpoint.purpose, result) || checkpoint.validate == nil ||
		!checkpoint.validate(bounded) {
		return wikiTemplateResult{}, false
	}
	return result, true
}

// Bound each tenant/KB to at most 8 MiB of response text. A full or unavailable
// checkpoint store simply stops caching; it never changes task completion.
const wikiModelCheckpointSaveScript = `
if redis.call('HEXISTS',KEYS[1],ARGV[1]) == 1 or redis.call('HLEN',KEYS[1]) < tonumber(ARGV[3]) then
 redis.call('HSET',KEYS[1],ARGV[1],ARGV[2])
 redis.call('EXPIRE',KEYS[1],ARGV[4])
 return 1
end
return 0
`

func (s *wikiIngestService) saveWikiModelCheckpoint(
	ctx context.Context, checkpoint *wikiModelCheckpoint, result wikiTemplateResult,
) {
	if checkpoint == nil || ctx.Err() != nil || !validWikiModelCheckpoint(checkpoint.purpose, result) {
		return
	}
	data, err := json.Marshal(result)
	if err != nil || len(data) > wikiModelCheckpointMaxBytes {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if checkpoint.validate == nil || !checkpoint.validate(bounded) {
		return
	}
	// No detached write may survive loss of source or generation ownership.
	_ = s.redisClient.Eval(bounded, wikiModelCheckpointSaveScript, []string{checkpoint.key},
		checkpoint.field, data, wikiModelCheckpointMaxEntries, int64(wikiModelCheckpointTTL/time.Second)).Err()
}
