package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"

	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/x448/float16"
)

type exactPersonalEmbedder struct {
	embedding.Embedder
	consumer *exactPersonalConsumer
}

func (c *exactPersonalConsumer) registerIndexInputs(
	ctx context.Context, knowledge *types.Knowledge, chunks []*types.Chunk, indices []*types.IndexInfo,
) error {
	if knowledge == nil || knowledge.ID != c.source.knowledge || knowledge.TenantID != c.source.tenant ||
		knowledge.KnowledgeBaseID != c.source.kb || len(indices) != len(chunks) {
		return errPersonalExactDocument
	}
	if err := c.verifyParserChunks(chunks); err != nil {
		return err
	}
	inputs := make(map[[32]byte]bool, len(indices))
	if c.indexContents == nil {
		c.indexContents = make(map[string]string)
	}
	for i, index := range indices {
		chunk := chunks[i]
		if index == nil || index.KnowledgeID != c.source.knowledge || index.KnowledgeBaseID != c.source.kb ||
			index.SourceType != types.ChunkSourceType || index.SourceID != chunk.ID || index.ChunkID != chunk.ID ||
			index.Content != buildKnowledgeIndexContent(knowledge, chunk.EmbeddingContent()) {
			return errPersonalExactDocument
		}
		inputs[retriever.EmbeddingInputDigest(ctx, index.Content)] = true
		if previous, exists := c.indexContents[index.SourceID]; exists && previous != index.Content {
			return errPersonalExactDocument
		}
		c.indexContents[index.SourceID] = index.Content
	}
	c.embeddingInputs = inputs
	return nil
}

func (e *exactPersonalEmbedder) check(ctx context.Context, texts []string) error {
	if e.consumer == nil || len(e.consumer.embeddingInputs) == 0 {
		return types.ErrExactDocumentScope
	}
	for _, text := range texts {
		if !e.consumer.embeddingInputs[sha256.Sum256([]byte(text))] {
			return types.ErrExactDocumentScope
		}
	}
	_, err := e.consumer.recovery.revalidateExactPersonalDocument(ctx, e.consumer.source)
	if err != nil {
		return errors.Join(types.ErrExactDocumentScope, err)
	}
	return err
}

func (e *exactPersonalEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := e.check(ctx, []string{text}); err != nil {
		return nil, err
	}
	vector, err := e.Embedder.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	if err = e.accept(ctx, []string{text}, [][]float32{vector}); err != nil {
		return nil, err
	}
	return vector, nil
}

func (e *exactPersonalEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	if err := e.check(ctx, texts); err != nil {
		return nil, err
	}
	vectors, err := e.Embedder.BatchEmbed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if err = e.accept(ctx, texts, vectors); err != nil {
		return nil, err
	}
	return vectors, nil
}

func (e *exactPersonalEmbedder) BatchEmbedWithPool(
	ctx context.Context, _ embedding.Embedder, texts []string,
) ([][]float32, error) {
	if err := e.check(ctx, texts); err != nil {
		return nil, err
	}
	// Pool workers retain the checking wrapper instead of a raw provider.
	vectors, err := e.Embedder.BatchEmbedWithPool(ctx, e, texts)
	if err != nil {
		return nil, err
	}
	if err = e.accept(ctx, texts, vectors); err != nil {
		return nil, err
	}
	return vectors, nil
}

// A successful HTTP response is not publication authority. Recheck the live
// source after the model call as well, and reject malformed results before
// they reach the normal PostgreSQL writer.
func (e *exactPersonalEmbedder) accept(ctx context.Context, texts []string, vectors [][]float32) error {
	if len(texts) == 0 || len(vectors) != len(texts) {
		return types.ErrExactDocumentScope
	}
	dimension := len(vectors[0])
	if dimension == 0 {
		return types.ErrExactDocumentScope
	}
	for _, vector := range vectors {
		if len(vector) != dimension {
			return types.ErrExactDocumentScope
		}
		for _, value := range vector {
			if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
				return types.ErrExactDocumentScope
			}
		}
	}
	if err := e.check(ctx, texts); err != nil {
		return err
	}
	e.consumer.resultMu.Lock()
	defer e.consumer.resultMu.Unlock()
	if e.consumer.modelVectors == nil {
		e.consumer.modelVectors = make(map[[32]byte][32]byte)
	}
	for i, text := range texts {
		key := sha256.Sum256([]byte(text))
		digest := exactHalfVectorDigest(vectors[i])
		if previous, exists := e.consumer.modelVectors[key]; exists && previous != digest {
			return types.ErrExactDocumentScope
		}
		e.consumer.modelVectors[key] = digest
	}
	return nil
}

// PostgreSQL stores halfvec; compare the exact half-precision representation
// rather than requiring a float32 round-trip the database cannot preserve.
func exactHalfVectorDigest(vector []float32) [32]byte {
	data := make([]byte, len(vector)*2)
	for i, value := range vector {
		binary.BigEndian.PutUint16(data[i*2:], uint16(float16.Fromfloat32(value)))
	}
	return sha256.Sum256(data)
}
