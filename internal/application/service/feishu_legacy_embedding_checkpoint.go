package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
)

type legacyEmbeddingCheckpointStore interface {
	StartEmbeddingCheckpoint(context.Context, *types.DataSource, *types.FeishuLegacyAttempt,
		types.FeishuLegacyEmbeddingCheckpoint) error
	ReusableEmbeddingCheckpoint(
		context.Context, *types.DataSource, *types.FeishuLegacyAttempt,
	) (map[string][]float32, error)
	SaveEmbeddingCheckpoint(context.Context, *types.DataSource, *types.FeishuLegacyAttempt, []string, [][]float32) error
	Renew(context.Context, *types.DataSource, *types.FeishuLegacyAttempt) error
}

func embedLegacyCheckpointed(
	ctx context.Context, store legacyEmbeddingCheckpointStore, ds *types.DataSource,
	owner *types.FeishuLegacyAttempt, model embedding.Embedder,
	checkpoint types.FeishuLegacyEmbeddingCheckpoint, texts []string,
) ([][]float32, error) {
	if len(texts) != len(checkpoint.Inputs) {
		return nil, fmt.Errorf("embedding checkpoint input cardinality mismatch")
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if feishusafety.Scoped(ctx) {
			if err := feishusafety.Default().Check(ctx); err != nil {
				return err
			}
		}
		return store.Renew(ctx, ds, owner)
	}
	if err := check(); err != nil {
		return nil, err
	}
	if err := store.StartEmbeddingCheckpoint(ctx, ds, owner, checkpoint); err != nil {
		return nil, err
	}
	cache, err := store.ReusableEmbeddingCheckpoint(ctx, ds, owner)
	if err != nil {
		return nil, err
	}
	saved := map[string]bool{}
	completed, reused := 0, 0
	multiplicity := map[string]int{}
	for i, text := range texts {
		digest := repository.LegacyEmbeddingInputHash(text)
		if digest != checkpoint.Inputs[i].SHA256 {
			return nil, fmt.Errorf("embedding checkpoint input digest mismatch")
		}
		multiplicity[digest]++
		if _, ok := cache[digest]; ok {
			reused++
		}
	}
	logger.Infof(ctx, "Feishu embedding checkpoint source=%s attempt=%s total=%d reused=%d unique_inputs=%d",
		ds.ID, owner.ID, len(texts), reused, len(multiplicity))
	save := func(check context.Context, inputs []string, vectors [][]float32) error {
		if feishusafety.Scoped(check) {
			if err := feishusafety.Default().Check(check); err != nil {
				return err
			}
		}
		if err := store.SaveEmbeddingCheckpoint(check, ds, owner, inputs, vectors); err != nil {
			return err
		}
		for i, text := range inputs {
			digest := repository.LegacyEmbeddingInputHash(text)
			cache[digest] = vectors[i]
			if !saved[digest] {
				completed += multiplicity[digest]
				saved[digest] = true
			}
		}
		logger.Infof(check, "Feishu embedding checkpoint saved source=%s attempt=%s completed=%d total=%d reused=%d",
			ds.ID, owner.ID, completed, len(texts), reused)
		return nil
	}
	ctx = embedding.WithBatchCheckpoint(ctx, save)
	for start := 0; start < len(texts); start += 20 {
		end := min(start+20, len(texts))
		// One exact input is embedded once even if several chunks repeat it.
		// Every original chunk still receives its own persisted vector row.
		batch := []string{}
		seen := map[string]bool{}
		for _, text := range texts[start:end] {
			digest := repository.LegacyEmbeddingInputHash(text)
			if !seen[digest] {
				batch = append(batch, text)
				seen[digest] = true
			}
		}
		for retry := 0; ; retry++ {
			if err = check(); err != nil {
				return nil, err
			}
			missing := []string{}
			for _, text := range batch {
				if _, ok := cache[repository.LegacyEmbeddingInputHash(text)]; !ok {
					missing = append(missing, text)
				}
			}
			if len(missing) == 0 {
				break
			}
			vectors, callErr := model.BatchEmbed(ctx, missing)
			if callErr == nil {
				if len(vectors) != len(missing) {
					return nil, fmt.Errorf("embedding checkpoint response cardinality mismatch")
				}
				// Non-protocol embedders retain a safe whole-call fallback. The
				// protocol callback has already saved each physical response.
				unpersisted, values := []string{}, [][]float32{}
				for i, text := range missing {
					if !saved[repository.LegacyEmbeddingInputHash(text)] {
						unpersisted = append(unpersisted, text)
						values = append(values, vectors[i])
					}
				}
				if len(unpersisted) > 0 {
					if err = save(ctx, unpersisted, values); err != nil {
						return nil, err
					}
				}
				break
			}
			if !errors.Is(callErr, feishusafety.ErrBudgetBusy) || retry >= 2 {
				return nil, callErr
			}
			logger.Warnf(ctx, "Feishu embedding backpressure source=%s attempt=%s retry=%d completed=%d total=%d error=%v",
				ds.ID, owner.ID, retry+1, completed, len(texts), callErr)
			timer := time.NewTimer(time.Duration(retry+1) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		inputs, vectors := []string{}, [][]float32{}
		for _, text := range batch {
			digest := repository.LegacyEmbeddingInputHash(text)
			if !saved[digest] {
				inputs = append(inputs, text)
				vectors = append(vectors, cache[digest])
			}
		}
		if len(inputs) > 0 {
			if err = save(ctx, inputs, vectors); err != nil {
				return nil, err
			}
		}
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		out[i] = cache[repository.LegacyEmbeddingInputHash(text)]
	}
	return out, nil
}
