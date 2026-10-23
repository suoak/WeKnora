package embedding

import (
	"context"
	"fmt"
)

// BatchEmbedBounded bounds a direct staging call without introducing a second
// concurrency pool. The model may split further to honour a lower gateway cap.
// Results are usable only when EVERY batch succeeds with the expected cardinality.
func BatchEmbedBounded(ctx context.Context, model Embedder, texts []string, limit int) ([][]float32, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("embedding batch limit must be positive")
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(start+limit, len(texts))
		vectors, err := model.BatchEmbed(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		if len(vectors) != end-start {
			return nil, fmt.Errorf("embedding model returned %d vectors for %d inputs", len(vectors), end-start)
		}
		out = append(out, vectors...)
	}
	return out, nil
}
