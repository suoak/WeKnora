package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
)

func sanitizeWikiGenerationMessages(ctx context.Context, msgs []chat.Message) ([]chat.Message, error) {
	if _, ok := repository.WikiMCPGenerationTenant(ctx); !ok {
		return msgs, nil
	}
	out := append([]chat.Message(nil), msgs...)
	for i, m := range out {
		if len(m.MultiContent) > 0 || len(m.Images) > 0 || len(m.ToolCalls) > 0 || m.ToolCallID != "" || m.ReasoningContent != "" || len(m.ReasoningMetadata) > 0 {
			return nil, repository.ErrWikiMCPUnverified
		}
		clean, e := access.SanitizePortalText(m.Content)
		if e != nil {
			return nil, e
		}
		out[i].Content = clean
	}
	return out, nil
}

// Constructed by the existing model registry; never uses a client-supplied
// model or tenant. Ordinary operations retain their existing embedding guards.
type wikiGuardedEmbedder struct {
	embedding.Embedder
	gate   legacyModelInputGate
	tenant uint64
}

func (m *wikiGuardedEmbedder) texts(ctx context.Context, texts []string) ([]string, error) {
	if _, ok := repository.WikiMCPGenerationTenant(ctx); !ok {
		return texts, nil
	}
	if e := m.gate.CheckLegacyModelInputs(ctx, m.tenant); e != nil {
		return nil, e
	}
	out := make([]string, len(texts))
	for i, t := range texts {
		v, e := access.SanitizePortalText(t)
		if e != nil {
			return nil, e
		}
		out[i] = v
	}
	return out, nil
}
func (m *wikiGuardedEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	texts, e := m.texts(ctx, []string{text})
	if e != nil {
		return nil, e
	}
	return m.Embedder.Embed(ctx, texts[0])
}
func (m *wikiGuardedEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	clean, e := m.texts(ctx, texts)
	if e != nil {
		return nil, e
	}
	return m.Embedder.BatchEmbed(ctx, clean)
}
func (m *wikiGuardedEmbedder) BatchEmbedWithPool(ctx context.Context, model embedding.Embedder, texts []string) ([][]float32, error) {
	if _, ok := repository.WikiMCPGenerationTenant(ctx); ok {
		return m.BatchEmbed(ctx, texts)
	}
	return m.Embedder.BatchEmbedWithPool(ctx, model, texts)
}
