package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestWikiModelCheckpointSurvivesWorkerReplacementAndExpires(t *testing.T) {
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	ctx := context.Background()
	checkpoint := &wikiModelCheckpoint{
		key:      "wiki:model-checkpoint:v1:{7:kb}",
		field:    "exact-request",
		purpose:  "wiki_chunk_citation",
		validate: func(context.Context) bool { return true },
	}
	result := wikiTemplateResult{Content: `{"citations":{"concept/topic":["c001"]}}`, FinishReason: "stop"}
	before := &wikiIngestService{redisClient: rc}
	before.saveWikiModelCheckpoint(ctx, checkpoint, result)
	after := &wikiIngestService{redisClient: rc}
	read, hit := after.loadWikiModelCheckpoint(ctx, checkpoint)
	require.True(t, hit)
	require.Equal(t, result, read)
	other := *checkpoint
	other.key = "wiki:model-checkpoint:v1:{8:kb}"
	_, hit = after.loadWikiModelCheckpoint(ctx, &other)
	require.False(t, hit, "another tenant must not obtain a checkpoint")
	other = *checkpoint
	other.field = "changed-input-or-model"
	_, hit = after.loadWikiModelCheckpoint(ctx, &other)
	require.False(t, hit)
	server.FastForward(wikiModelCheckpointTTL + time.Second)
	_, hit = after.loadWikiModelCheckpoint(ctx, checkpoint)
	require.False(t, hit)
}

func TestWikiModelCheckpointRefusesIncompleteResponsesAndBoundsStorage(t *testing.T) {
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	svc := &wikiIngestService{redisClient: rc}
	ctx := context.Background()
	checkpoint := &wikiModelCheckpoint{
		key: "wiki:model-checkpoint:v1:{7:kb}", purpose: "wiki_chunk_citation",
		validate: func(context.Context) bool { return true },
	}
	for _, result := range []wikiTemplateResult{
		{Content: `{"citations":`, FinishReason: "length"},
		{Content: `{"citations":{}}`, FinishReason: "length"},
		{Content: `{}`, FinishReason: "stop"},
		{Content: strings.Repeat("x", wikiModelCheckpointMaxBytes+1), FinishReason: "stop"},
	} {
		svc.saveWikiModelCheckpoint(ctx, checkpoint, result)
	}
	require.False(t, server.Exists(checkpoint.key))
	valid := wikiTemplateResult{Content: `{"citations":{}}`, FinishReason: "stop"}
	for i := range wikiModelCheckpointMaxEntries + 10 {
		checkpoint.field = fmt.Sprint(i)
		svc.saveWikiModelCheckpoint(ctx, checkpoint, valid)
	}
	require.EqualValues(t, wikiModelCheckpointMaxEntries, rc.HLen(ctx, checkpoint.key).Val())
	// Malformed data is a cache miss, never input to Wiki generation.
	checkpoint.field = "corrupt"
	require.NoError(t, rc.HSet(ctx, checkpoint.key, checkpoint.field, "invalid").Err())
	_, hit := svc.loadWikiModelCheckpoint(ctx, checkpoint)
	require.False(t, hit)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, hit = svc.loadWikiModelCheckpoint(canceled, checkpoint)
	require.False(t, hit)
	server.Close()
	_, hit = svc.loadWikiModelCheckpoint(ctx, checkpoint)
	require.False(t, hit, "Redis loss must fall back to ordinary generation")
}

func TestWikiModelCheckpointNeedsServerIssuedGenerationScope(t *testing.T) {
	svc := &wikiIngestService{}
	require.Nil(t, svc.wikiModelCheckpoint(context.Background(), "model", "wiki_summary", "request"))
	require.False(
		t,
		validWikiModelCheckpoint("wiki_page_modify", wikiTemplateResult{Content: "body", FinishReason: "stop"}),
	)
}
