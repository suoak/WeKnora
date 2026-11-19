package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWikiModelCheckpointScopeIsBoundToPrivateGenerationProof(t *testing.T) {
	_, _, _, ok := WikiModelCheckpointScope(context.Background())
	require.False(t, ok)
	proof := &wikiGenerationProof{
		Tenant: 7, KB: "kb", DependencySHA256: strings.Repeat("a", 64),
		Sources: map[string]string{"document": "version"},
	}
	ctx := context.WithValue(context.Background(), wikiGenerationProofKey{}, proof)
	tenant, kb, digest, ok := WikiModelCheckpointScope(ctx)
	require.True(t, ok)
	require.EqualValues(t, 7, tenant)
	require.Equal(t, "kb", kb)
	require.Equal(t, proof.DependencySHA256, digest)
	proof.Sources = nil
	_, _, _, ok = WikiModelCheckpointScope(ctx)
	require.False(t, ok)
}
