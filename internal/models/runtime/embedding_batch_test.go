package runtime

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/models"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestEmbeddingGatewayLimitP0(t *testing.T) {
	for _, tc := range []struct {
		provider, limit string
		want            int
		invalid         bool
	}{
		{"generic", "", 0, false},
		{"generic", "20", 20, false},
		{"generic", "10", 10, false},
		{"openai", "4000", 2048, false},
		{"openai", "10", 10, false},
		{"generic", "0", 0, true},
		{"generic", "-1", 0, true},
		{"generic", "x", 0, true},
	} {
		t.Run(tc.provider+"/"+tc.limit, func(t *testing.T) {
			r, err := Resolve(
				Ref{
					Provider:  tc.provider,
					Model:     "synthetic",
					BaseURL:   "https://example.com/v1",
					ModelType: types.ModelTypeEmbedding,
					Extra:     map[string]string{models.ExtraEmbeddingMaxBatchSize: tc.limit},
				},
			)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, r.Embeddings.MaxBatchSize)
		})
	}
}
