package service

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPersonalExactEmbeddingRepeatedInput(t *testing.T) {
	f, proof, ctx := exactExecutionFixture(t)
	text := "repeated workbook table header"
	key := sha256.Sum256([]byte(text))
	consumer := &exactPersonalConsumer{
		recovery: f.p, source: proof, embeddingInputs: map[[32]byte]bool{key: true},
	}
	embedder := &exactPersonalEmbedder{consumer: consumer}
	// Two genuine provider responses for identical text need not be bit identical.
	require.NoError(t, embedder.accept(ctx, []string{text, text}, [][]float32{{1, 0}, {1, 0.01}}))
	// Pool workers and their caller both record the same results.
	require.NoError(t, embedder.accept(ctx, []string{text, text}, [][]float32{{1, 0}, {1, 0.01}}))
	require.Len(t, consumer.modelVectors[key], 2)
	require.True(t, consumer.modelVectors[key][exactHalfVectorDigest([]float32{1, 0})])
	require.True(t, consumer.modelVectors[key][exactHalfVectorDigest([]float32{1, 0.01})])
	require.False(t, consumer.modelVectors[key][exactHalfVectorDigest([]float32{0, 1})])
	require.Error(t, embedder.accept(ctx, []string{"unproved input"}, [][]float32{{1, 0}}))
	require.Len(t, consumer.modelVectors, 1)
}
