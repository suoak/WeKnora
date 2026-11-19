//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/agent"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type wikiCheckpointModels struct {
	interfaces.ModelService
	model *types.Model
}

func (s *wikiCheckpointModels) GetModelByID(context.Context, string) (*types.Model, error) {
	return s.model, nil
}

type wikiCheckpointChat struct {
	templateCaptureChatModel
	calls int
}

func (m *wikiCheckpointChat) Chat(ctx context.Context, messages []chat.Message,
	opts *chat.ChatOptions,
) (*types.ChatResponse, error) {
	m.calls++
	return m.templateCaptureChatModel.Chat(ctx, messages, opts)
}

func TestWikiModelCheckpointNativeReusesExactRequestAcrossWorkerReplacement(t *testing.T) {
	t.Setenv("WIKI_SELECTION_NATIVE_TEST", "true")
	f := prepareWikiSelectionFixture(t)
	ctx, _, err := repository.BeginWikiDocumentGeneration(context.Background(),
		repository.NewWikiPageRepository(f.DB), "public-two")
	require.NoError(t, err)
	ctx = context.WithValue(ctx, types.TenantIDContextKey, uint64(1))
	server := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	models := &wikiCheckpointModels{model: &types.Model{
		ID: "capture", TenantID: 1,
		Name: "checkpoint-model", UpdatedAt: time.Now().UTC(),
	}}
	model := &wikiCheckpointChat{templateCaptureChatModel: templateCaptureChatModel{response: `{"citations":{}}`}}
	data := map[string]string{"CandidateSlugs": "candidate", "ChunksXML": "current chunk", "Language": "English"}
	pages := &wikiPageService{repo: repository.NewWikiPageRepository(f.DB)}
	service := &wikiIngestService{redisClient: rc, modelService: models, wikiService: pages}
	first, err := service.generateWithTemplate(ctx, model, agent.WikiChunkCitationPrompt, data)
	require.NoError(t, err)
	require.Equal(t, 1, model.calls)
	require.Len(t, server.Keys(), 1, "verified map response must have a private checkpoint")
	restarted := &wikiIngestService{redisClient: rc, modelService: models, wikiService: pages}
	second, err := restarted.generateWithTemplate(ctx, model, agent.WikiChunkCitationPrompt, data)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, model.calls, "worker replacement must not repeat the same provider request")
	data["ChunksXML"] = "changed current chunk"
	_, err = restarted.generateWithTemplate(ctx, model, agent.WikiChunkCitationPrompt, data)
	require.NoError(t, err)
	require.Equal(t, 2, model.calls, "changed model inputs must not use a previous result")
	models.model.UpdatedAt = models.model.UpdatedAt.Add(time.Second)
	_, err = restarted.generateWithTemplate(ctx, model, agent.WikiChunkCitationPrompt, data)
	require.NoError(t, err)
	require.Equal(t, 3, model.calls, "model configuration changes must invalidate the result")
	_, err = restarted.generateWithTemplate(context.Background(), model, agent.WikiChunkCitationPrompt, data)
	require.NoError(t, err)
	require.Equal(t, 4, model.calls, "an unproved context cannot read the checkpoint")
	checkpoint := restarted.wikiModelCheckpoint(ctx, "capture", "wiki_chunk_citation", "revocation-test")
	require.NotNil(t, checkpoint)
	restarted.saveWikiModelCheckpoint(ctx, checkpoint, wikiTemplateResult{Content: `{"citations":{}}`})
	_, hit := restarted.loadWikiModelCheckpoint(ctx, checkpoint)
	require.True(t, hit)
	require.NoError(t, f.DB.Exec("UPDATE knowledges SET parse_status='failed' WHERE id='public-two-doc'").Error)
	_, hit = restarted.loadWikiModelCheckpoint(ctx, checkpoint)
	require.False(t, hit, "a retained proof must not permit reuse after current source verification fails")
}
