package service

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/models/vlm"
)

func legacyImageRoundContext(ctx context.Context) (context.Context, context.CancelFunc) {
	budget := 90 * time.Second
	if feishusafety.ProviderFailuresIsolated(ctx) {
		budget = vlm.RequestTimeout()
	}
	// WithTimeout also preserves an earlier source-job deadline. It neither
	// grants admission nor changes provider accounting or concurrency limits.
	return context.WithTimeout(ctx, budget)
}

func legacyImageRoundError(ctx context.Context, roundErr error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if errors.Is(roundErr, context.DeadlineExceeded) {
		return &feishuStagingRefusal{reason: "image_enrichment_timeout"}
	}
	// Provider messages and image contents remain inside the private context.
	return &feishuStagingRefusal{reason: "image_enrichment_failed"}
}
