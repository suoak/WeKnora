package feishusynccontrol

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestApprovedSourcesCanaryOutboundUsesLiveClaim(t *testing.T) {
	for _, embedding := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "embedding"}[embedding], func(t *testing.T) {
			f := newRecoveryFixture(t)
			ctx := context.Background()
			facts := f.facts
			var released atomic.Bool
			var changed atomic.Bool
			f.m.facts = func(work context.Context, _ string) (RecoveryFacts, error) {
				current := facts
				if _, claimed := types.SyncExecutionFromContext(work); claimed && released.Load() {
					current.Eligible = false
				}
				if _, claimed := types.SyncExecutionFromContext(work); claimed && changed.Load() {
					current.FactsSHA256 = strings.Repeat("f", 64)
				}
				return current, nil
			}
			require.NoError(t, f.m.Reset(ctx, f.p, f.approve(t, "reset"), "operator"))
			require.NoError(t, f.c.Step(ctx))
			calls := 0
			transport := feishusafety.Transport{Embedding: embedding, Base: continuityRoundTripper(
				func(*http.Request) (*http.Response, error) {
					calls++
					return &http.Response{
						StatusCode: 200, Header: http.Header{},
						Body: io.NopCloser(strings.NewReader(`{"ok":true}`)),
					}, nil
				})}
			request := func(work context.Context) error {
				req, err := http.NewRequestWithContext(work, http.MethodPost, "https://gateway.invalid/v1/request",
					strings.NewReader(`{"input":["synthetic"]}`))
				require.NoError(t, err)
				response, err := transport.RoundTrip(req)
				if response != nil {
					_ = response.Body.Close()
				}
				return err
			}
			err := f.m.Canary(ctx, f.p, f.approve(t, "canary"), "operator",
				func(work context.Context, bind func(string, int64) error) error {
					run := uuid.NewString()
					require.NoError(t, bind(run, 1))
					work = feishusafety.WithScope(work, facts.SourceID)
					work = feishusafety.WithRecoveryExecution(work, facts.ConfigSHA256, run, 1)
					require.NoError(t, f.c.Guard.Check(work), "pre-claim producer admission remains valid")
					require.ErrorIs(t, request(work), feishusafety.ErrClosed, "actual outbound requires a SQL claim")
					execution := types.SyncExecution{
						DataSourceID: facts.SourceID,
						SyncLogID:    run,
						TenantID:     1,
						Generation:   1,
					}
					claimed := types.WithSyncExecution(work, execution)
					require.NoError(t, request(claimed))
					require.Equal(t, 1, calls)
					released.Store(true)
					require.ErrorIs(
						t,
						request(claimed),
						feishusafety.ErrClosed,
						"released SQL claim rejects an unchanged context",
					)
					released.Store(false)
					execution.Generation = 2
					require.ErrorIs(t, request(types.WithSyncExecution(work, execution)), feishusafety.ErrClosed)
					changed.Store(true)
					require.ErrorIs(
						t,
						request(claimed),
						feishusafety.ErrClosed,
						"authority changes reject the next request",
					)
					changed.Store(false)
					require.Equal(t, 1, calls, "denied requests never reach the provider")
					return nil
				})
			require.NoError(t, err)
			require.EqualValues(t, 6, f.r.LLen(ctx, "asynq:{sync}:pending").Val())
		})
	}
}
