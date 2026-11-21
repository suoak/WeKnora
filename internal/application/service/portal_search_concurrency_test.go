package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func globalPortalSearchFixture(t *testing.T, extra int) (*fixture.Fixture, *PortalMCPKnowledge, context.Context, int) {
	t.Helper()
	f := fixture.New(t)
	f.PreparePublished(t)
	for i := range extra {
		require.NoError(t, f.DB.Exec(`INSERT INTO knowledge_bases
			(id,name,tenant_id,embedding_model_id,summary_model_id,type)
			SELECT ?,?,tenant_id,embedding_model_id,summary_model_id,type
			FROM knowledge_bases WHERE id='public-kb'`,
			fmt.Sprintf("parallel-%02d", i), fmt.Sprintf("synthetic %02d", i)).Error)
	}
	s := publishedServiceM7(t, f)
	s.Embed = func(context.Context, access.PortalKBDescriptor, string) ([]float32, error) {
		return []float32{1}, nil
	}
	ctx := fixture.Key("A", 1, 1, true)
	targets, err := s.Resolve(ctx, nil)
	require.NoError(t, err)
	require.Len(t, targets, extra+3)
	return f, s, ctx, len(targets)
}

type globalPortalSearchReply struct {
	result *PortalMCPSearchResult
	err    error
}

func TestPortalGlobalSearchOverlapsIndependentTargetsWithBoundedFanout(t *testing.T) {
	for _, mode := range []string{"keyword", "semantic", "hybrid", "grep"} {
		t.Run(mode, func(t *testing.T) {
			_, s, base, count := globalPortalSearchFixture(t, 62)
			ctx, cancel := context.WithTimeout(base, 20*time.Second)
			defer cancel()
			started := make(chan string, count*2)
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			var active, peak atomic.Int32
			var mu sync.Mutex
			calls := map[string]int{}
			s.Engine = func(_ context.Context, d access.PortalKBDescriptor) (access.DocumentRetriever, error) {
				return portalV1Driver{f: func(
					ctx context.Context, _ types.RetrieveParams,
				) ([]*types.RetrieveResult, error) {
					n := active.Add(1)
					defer active.Add(-1)
					for old := peak.Load(); n > old; old = peak.Load() {
						if peak.CompareAndSwap(old, n) {
							break
						}
					}
					mu.Lock()
					calls[d.ID]++
					mu.Unlock()
					started <- d.ID
					select {
					case <-release:
						return nil, nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}}, nil
			}
			finished := make(chan struct{})
			reply := make(chan globalPortalSearchReply, 1)
			go func() {
				defer close(finished)
				result, err := s.Search(ctx, nil, "synthetic", mode, 1)
				reply <- globalPortalSearchReply{result, err}
			}()
			defer func() { cancel(); unblock(); <-finished }()
			// A serial coordinator cannot reach this barrier. Do not use elapsed
			// time assertions: every occupied worker has to reach the driver.
			for range portalSearchTargetConcurrency {
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal("independent targets did not overlap")
				}
			}
			require.EqualValues(t, portalSearchTargetConcurrency, active.Load())
			unblock()
			got := <-reply
			require.NoError(t, got.err)
			require.Equal(t, "complete", got.result.Status)
			require.Equal(t, count, got.result.Searched)
			require.Zero(t, got.result.Failed)
			require.Empty(t, got.result.Results)
			require.Zero(t, active.Load(), "all workers must finish before returning")
			require.LessOrEqual(t, peak.Load(), int32(portalSearchTargetConcurrency))
			require.Len(t, calls, count, "no authorized target is truncated to meet the deadline")
			for _, n := range calls {
				want := 1
				if mode == "hybrid" {
					want = 2
				}
				require.Equal(t, want, n)
			}
		})
	}
}

func TestPortalGlobalSearchConcurrentInvalidationDiscardsEveryResult(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"key_revoked", "UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1"},
		{"portal_withdrawn", "UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=99"},
		{"raw_chunk_mutation", "UPDATE chunks SET content='synthetic changed body' " +
			"WHERE knowledge_base_id='public-kb'"},
		{"caller_cancelled", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, s, base, _ := globalPortalSearchFixture(t, 5)
			ctx, cancel := context.WithTimeout(base, 15*time.Second)
			defer cancel()
			started := make(chan struct{}, 8)
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			s.Engine = func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) {
				return portalV1Driver{f: func(
					ctx context.Context, _ types.RetrieveParams,
				) ([]*types.RetrieveResult, error) {
					started <- struct{}{}
					select {
					case <-release:
						return nil, nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}}, nil
			}
			finished := make(chan struct{})
			reply := make(chan globalPortalSearchReply, 1)
			go func() {
				defer close(finished)
				result, err := s.Search(ctx, nil, "synthetic", "keyword", 1)
				reply <- globalPortalSearchReply{result, err}
			}()
			defer func() { cancel(); unblock(); <-finished }()
			for range portalSearchTargetConcurrency {
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal("retrieval workers did not start")
				}
			}
			if tc.sql == "" {
				cancel()
			} else {
				require.NoError(t, f.DB.Exec(tc.sql).Error)
			}
			unblock()
			got := <-reply
			require.Error(t, got.err)
			require.Nil(t, got.result, "concurrent invalidation must never expose a successful partial response")

		})
	}
}

func TestPortalGlobalSearchRetainsFailedTargetAccounting(t *testing.T) {
	_, s, ctx, count := globalPortalSearchFixture(t, 5)
	s.Engine = func(_ context.Context, d access.PortalKBDescriptor) (access.DocumentRetriever, error) {
		if d.ID == "parallel-00" {
			return nil, errors.New("synthetic unavailable provider")
		}
		return portalV1Driver{f: func(context.Context, types.RetrieveParams) ([]*types.RetrieveResult, error) {
			return nil, nil
		}}, nil
	}
	result, err := s.Search(ctx, nil, "synthetic", "keyword", 1)
	require.NoError(t, err)
	require.Equal(t, "partial", result.Status)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, count-1, result.Searched)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	result, err = s.Search(ctx, nil, "synthetic", "keyword", 1)
	require.Error(t, err)
	require.Nil(t, result)
}
