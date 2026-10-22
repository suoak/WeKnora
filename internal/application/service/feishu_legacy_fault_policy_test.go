package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	feishu "github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestIncrementalLegacyAvailabilityIsNotConfirmedWithdrawal(t *testing.T) {
	for _, kind := range []feishu.ErrorCategory{feishu.ErrorCategoryRateLimit, feishu.ErrorCategoryTransient, feishu.ErrorCategoryAuth, feishu.ErrorCategoryPermission, feishu.ErrorCategoryNotFound, feishu.ErrorCategoryUnknownAPI} {
		t.Run(string(kind), func(t *testing.T) {
			err := &feishu.APIError{Category: kind}
			want := kind != feishu.ErrorCategoryRateLimit && kind != feishu.ErrorCategoryTransient
			require.Equal(t, want, legacyFetchNeedsHold(fmt.Errorf("list failed: %w", err)))
			require.Equal(t, want, legacyItemNeedsHold(&types.FetchedItem{Metadata: feishu.FeishuErrorItemMeta(err, nil)}))
		})
	}
	require.False(t, legacyFetchNeedsHold(context.DeadlineExceeded))
	require.True(t, legacyFetchNeedsHold(errors.New("unknown listing failure")))
}
