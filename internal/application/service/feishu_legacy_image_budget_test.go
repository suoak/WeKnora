package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func imageBudgetGuard(t *testing.T, normal, closed bool) (*feishusafety.Guard, *redis.Client) {
	t.Helper()
	r := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = r.Close() })
	g, err := feishusafety.New(r, feishusafety.Config{
		Namespace: "image-budget", EndpointHost: "gateway.invalid", InFlight: 1,
		RPM: 30, RPS: 1, TPM: 30000, WaitBudgetDuration: time.Second, TokenBoundVerified: true,
	})
	require.NoError(t, err)
	g.SetProviderFailureIsolation(normal)
	if closed {
		g.CloseLocal()
	}
	feishusafety.InstallDefault(g)
	t.Cleanup(func() { feishusafety.InstallDefault(nil) })
	return g, r
}

func TestLegacyImageRoundBudgetPreservesAuthorizationAndModelConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, configured       string
		normal, scoped, closed bool
		want                   time.Duration
	}{
		{"strict", "180", false, true, false, 90 * time.Second},
		{"unscoped", "180", true, false, false, 90 * time.Second},
		{"normal-default", "", true, true, false, 180 * time.Second},
		{"normal-shorter", "30", true, true, false, 30 * time.Second},
		{"normal-configured", "300", true, true, false, 300 * time.Second},
		{"closed", "300", true, true, true, 90 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("VLM_HTTP_TIMEOUT_SECONDS", tc.configured)
			g, r := imageBudgetGuard(t, tc.normal, tc.closed)
			ctx := logger.WithPrivatePayload(context.Background())
			if tc.scoped {
				ctx = feishusafety.WithScope(ctx, "source")
			}
			start := time.Now()
			call, cancel := legacyImageRoundContext(ctx)
			defer cancel()
			deadline, ok := call.Deadline()
			require.True(t, ok)
			require.WithinDuration(t, start.Add(tc.want), deadline, time.Second)
			require.True(t, logger.PrivatePayload(call))
			require.Equal(t, tc.scoped, feishusafety.Scoped(call))
			// Selecting a deadline cannot mint outbound authority or reopen a guard.
			require.Zero(t, r.Exists(context.Background(), g.PermitKey()).Val())
			require.ErrorIs(t, g.Check(call), feishusafety.ErrClosed)
		})
	}
}

func TestLegacyImageRoundBudgetKeepsSourceDeadlineAndCancellation(t *testing.T) {
	t.Setenv("VLM_HTTP_TIMEOUT_SECONDS", "300")
	imageBudgetGuard(t, true, false)
	parent, stop := context.WithTimeout(feishusafety.WithScope(context.Background(), "source"), time.Second)
	defer stop()
	call, cancel := legacyImageRoundContext(parent)
	defer cancel()
	want, _ := parent.Deadline()
	actual, _ := call.Deadline()
	require.Equal(t, want, actual)
	stop()
	require.ErrorIs(t, call.Err(), context.Canceled)
	require.ErrorIs(t, legacyImageRoundError(parent, context.DeadlineExceeded), context.Canceled)
}

func TestLegacyImageRoundTimeoutHasSafeDocumentFailureCode(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	ds := &types.DataSource{Type: "feishu"}
	item := &types.FetchedItem{ExternalID: "source-file"}
	for _, tc := range []struct {
		err  error
		code string
	}{
		{context.DeadlineExceeded, "safe_staging_image_enrichment_timeout"},
		{nil, "safe_staging_image_enrichment_failed"},
	} {
		err := legacyImageRoundError(context.Background(), tc.err)
		require.Equal(t, tc.code, feishuDocumentFailureCode(ds, item, err))
		require.NotContains(t, err.Error(), "deadline")
		var refusal *feishuStagingRefusal
		require.True(t, errors.As(err, &refusal))
	}
}
