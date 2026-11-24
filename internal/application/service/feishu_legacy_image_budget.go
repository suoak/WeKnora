package service

import (
	"context"
	"errors"
)

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
