package service

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/models/embedding"
)

// legacyLeaseEmbedder renews only at bounded staging batch boundaries. Other
// model traffic retains its original model and concurrency implementation.
type legacyLeaseEmbedder struct {
	embedding.Embedder
	renew func(context.Context) error
}

func (m *legacyLeaseEmbedder) renewLease(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	check, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return m.renew(check)
}

func (m *legacyLeaseEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	if err := m.renewLease(ctx); err != nil {
		return nil, err
	}
	vectors, err := m.Embedder.BatchEmbed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if err = m.renewLease(ctx); err != nil {
		return nil, err
	}
	return vectors, nil
}
