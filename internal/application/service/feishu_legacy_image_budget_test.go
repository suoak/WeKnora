package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type imageBudgetFailureVLM struct{}

func (imageBudgetFailureVLM) GetModelID() string   { return "budget-test" }
func (imageBudgetFailureVLM) GetModelName() string { return "budget-test" }
func (imageBudgetFailureVLM) Predict(context.Context, [][]byte, string) (string, error) {
	return "", fmt.Errorf("private provider details: %w", context.DeadlineExceeded)
}

func TestLegacyImageRoundTimeoutHasSafeDocumentFailureCode(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	ds := &types.DataSource{Type: "feishu"}
	item := &types.FetchedItem{ExternalID: "source-file"}
	for _, action := range []types.ActionKind{types.ActionCaption, types.ActionObservation, types.ActionOCR} {
		t.Run(string(action), func(t *testing.T) {
			out := types.JSONMap{}
			ctx := logger.WithPrivatePayload(context.Background())
			err := (&ImageMultimodalService{}).executeActionRound(ctx, &types.ImageMultimodalPayload{},
				imageBudgetFailureVLM{}, nil, types.VLMConfig{}, &types.ImageInfo{}, out,
				ActionRound{Actions: []types.ActionKind{action}})
			require.ErrorIs(t, err, context.DeadlineExceeded)
			safe := legacyImageRoundError(ctx, err)
			require.Equal(t, "safe_staging_image_enrichment_timeout", feishuDocumentFailureCode(ds, item, safe))
			require.NotContains(t, safe.Error(), "private")
			var refusal *feishuStagingRefusal
			require.True(t, errors.As(safe, &refusal))
		})
	}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	require.ErrorIs(t, legacyImageRoundError(ctx, context.DeadlineExceeded), context.Canceled)
	require.Equal(t, "safe_staging_image_enrichment_failed",
		feishuDocumentFailureCode(ds, item, legacyImageRoundError(context.Background(), errors.New("private"))))
}
