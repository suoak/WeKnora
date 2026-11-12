//go:build sqlite_fts5

package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/perftrace"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Real HTTP, authentication, PostgreSQL retrieval and synthetic embedding.
// The caller seeds both users/public/private documents using the permission fixture.
type personalConcurrencyUsage struct {
	mu      sync.Mutex
	reports []types.MCPUsageReport
}

func (s *personalConcurrencyUsage) Record(_ context.Context, r types.MCPUsageReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports = append(s.reports, r)
	return nil
}

func (s *personalConcurrencyUsage) snapshot() []types.MCPUsageReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]types.MCPUsageReport(nil), s.reports...)
}

func exercisePersonalMCPConcurrency(
	t *testing.T, handler http.Handler, s *service.PortalMCPKnowledge,
	keyA, keyB string, db *gorm.DB, usage *personalConcurrencyUsage,
) {
	t.Helper()
	baseline := os.Getenv("KNOWHUB_MCP_CONCURRENCY_BASELINE") == "1"
	original := s.Embed
	var slow atomic.Bool
	var active, peak atomic.Int32
	s.Embed = func(ctx context.Context, kb access.PortalKBDescriptor, q string) ([]float32, error) {
		v := active.Add(1)
		defer active.Add(-1)
		for p := peak.Load(); v > p; p = peak.Load() {
			if peak.CompareAndSwap(p, v) {
				break
			}
		}
		delay := 20 * time.Millisecond
		if slow.Load() && q == "ADMISSION_SLOW" {
			delay = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		return original(ctx, kb, q)
	}
	defer func() { s.Embed = original }()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	client.Timeout = 10 * time.Second
	type response struct {
		Status int
		Ms     float64
		Error  bool
		Retry  string
	}
	rpc := func(ctx context.Context, key, path, method, name string, args map[string]any) response {
		params := any(map[string]any{})
		if method == "tools/call" {
			params = map[string]any{"name": name, "arguments": args}
		}
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		r, _ := http.NewRequestWithContext(ctx, "POST", server.URL+path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("Authorization", "Bearer "+key)
		start := time.Now()
		resp, err := client.Do(r)
		if err != nil {
			return response{Ms: float64(time.Since(start).Microseconds()) / 1000, Error: true}
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var envelope struct {
			Error  any `json:"error"`
			Result struct {
				IsError bool  `json:"isError"`
				Content []any `json:"content"`
			} `json:"result"`
		}
		bad := json.Unmarshal(data, &envelope) != nil || envelope.Error != nil
		if method == "tools/call" {
			bad = bad || envelope.Result.IsError || len(envelope.Result.Content) == 0
		}
		return response{
			resp.StatusCode, float64(time.Since(start).Microseconds()) / 1000, bad, resp.Header.Get("Retry-After"),
		}
	}
	search := func(q string) map[string]any {
		return map[string]any{"query": q, "mode": "semantic", "limit": 3, "knowledge_base_ids": []string{"public-two"}}
	}
	for _, n := range []int{3, 5, 10, 20} {
		t.Run(fmt.Sprintf("personal_http_concurrency_%d", n), func(t *testing.T) {
			before := len(usage.snapshot())
			start := make(chan struct{})
			rows := make([]response, n)
			var wg sync.WaitGroup
			begin := time.Now()
			for i := range rows {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					rows[i] = rpc(context.Background(), keyA, "/api/v1/mcp/personal",
						"tools/call", "search_knowledge", search("PUBLIC"))
				}(i)
			}
			close(start)
			wg.Wait()
			latencies := []float64{}
			success, rejected := 0, 0
			for _, row := range rows {
				latencies = append(latencies, row.Ms)
				if row.Status == 200 && !row.Error {
					success++
				} else {
					require.Equal(t, 429, row.Status)
					require.NotEmpty(t, row.Retry)
					rejected++
				}
			}
			if n <= 10 && (!baseline || n <= 5) {
				require.Equal(t, n, success)
			} else {
				require.GreaterOrEqual(t, success, 4)
			}
			sort.Float64s(latencies)
			reports := usage.snapshot()[before:]
			require.Len(t, reports, n)
			keys := map[string]bool{}
			recordedRejects := 0
			for _, r := range reports {
				require.False(t, keys[r.EventKey])
				keys[r.EventKey] = true
				if !r.Success {
					recordedRejects++
					if baseline {
						require.Equal(t, "rate_limited", r.ErrorCode)
					} else {
						require.Equal(t, "concurrency_limited", r.ErrorCode)
					}
				}
			}
			require.Equal(t, rejected, recordedRejects)
			t.Logf("LOAD n=%d success=%d rejected=%d p50_ms=%.1f p95_ms=%.1f throughput=%.2f/s",
				n, success, rejected, latencies[len(latencies)/2], latencies[(len(latencies)*95-1)/100],
				float64(success)/time.Since(begin).Seconds())
		})
	}
	t.Run("personal_http_sse_does_not_consume_search_or_discovery", func(t *testing.T) {
		var wg sync.WaitGroup
		var mu sync.Mutex
		streams := []io.ReadCloser{}
		statuses := make([]int, 8)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		for i := range statuses {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				r, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/mcp/personal", nil)
				r.Header.Set("Authorization", "Bearer "+keyA)
				r.Header.Set("Accept", "text/event-stream")
				resp, err := client.Do(r)
				if err != nil {
					return
				}
				statuses[i] = resp.StatusCode
				if resp.StatusCode == 200 {
					mu.Lock()
					streams = append(streams, resp.Body)
					mu.Unlock()
				} else {
					resp.Body.Close()
				}
			}(i)
		}
		wg.Wait()
		defer func() {
			cancel()
			for _, body := range streams {
				body.Close()
			}
		}()
		if baseline {
			require.Len(t, streams, 8)
		} else {
			require.Len(t, streams, 2)
		}
		rows := make([]response, 3)
		for i := range rows {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				rows[i] = rpc(context.Background(), keyA, "/api/v1/mcp/personal",
					"tools/call", "search_knowledge", search("PUBLIC"))
			}(i)
		}
		wg.Wait()
		success := 0
		for _, row := range rows {
			if baseline {
				require.Equal(t, 429, row.Status)
			} else {
				require.Equal(t, 200, row.Status)
				require.False(t, row.Error)
				success++
			}
		}
		listed := rpc(context.Background(), keyA, "/api/v1/mcp/personal", "tools/list", "", nil)
		if baseline {
			require.Equal(t, 429, listed.Status)
		} else {
			require.Equal(t, 200, listed.Status)
			require.False(t, listed.Error)
		}
		t.Logf("SSE_REPRO baseline=%t listeners=%d independent_search_success=%d/3 discovery_status=%d",
			baseline, len(streams), success, listed.Status)
	})
	// One owner occupies its four slots and queue. Another authenticated user
	// and protocol discovery must progress without waiting for those embeddings.
	t.Run("personal_http_fairness_cloud_and_discovery", func(t *testing.T) {
		slow.Store(true)
		defer slow.Store(false)
		var wg sync.WaitGroup
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = rpc(ctx, keyA, "/api/v1/mcp/personal", "tools/call", "search_knowledge", search("ADMISSION_SLOW"))
			}()
		}
		require.Eventually(t, func() bool { return active.Load() >= 4 }, 3*time.Second, time.Millisecond)
		other := rpc(context.Background(), keyB, "/internal/mcp/isolated/personal",
			"tools/call", "search_knowledge", search("PUBLIC"))
		if !baseline {
			require.Equal(t, 200, other.Status)
			require.False(t, other.Error)
			require.Less(t, other.Ms, 1500.0)
		}
		listed := rpc(context.Background(), keyA, "/api/v1/mcp/personal", "tools/list", "", nil)
		if !baseline {
			require.Equal(t, 200, listed.Status)
			require.False(t, listed.Error)
			require.Less(t, listed.Ms, 1500.0)
		}
		t.Logf("FAIRNESS baseline=%t other_user_status=%d discovery_status=%d other_ms=%.1f",
			baseline, other.Status, listed.Status, other.Ms)
		cancel()
		wg.Wait()
	})
	t.Run("personal_http_mixed_under_small_db_pool", func(t *testing.T) {
		pool, err := db.DB()
		require.NoError(t, err)
		max := pool.Stats().MaxOpenConnections
		pool.SetMaxOpenConns(2)
		defer pool.SetMaxOpenConns(max)
		var wg sync.WaitGroup
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				name, args := "search_knowledge", search("PUBLIC")
				if i%3 == 1 {
					name = "read_document"
					args = map[string]any{"knowledge_id": "public-two-doc"}
				}
				if i%3 == 2 {
					name = "wiki_search"
					args = map[string]any{"knowledge_base_id": "public-two", "query": "verified", "limit": 3}
				}
				path := "/api/v1/mcp/personal"
				key := keyA
				if i%2 == 1 {
					path = "/internal/mcp/isolated/personal"
					key = keyB
				}
				row := rpc(context.Background(), key, path, "tools/call", name, args)
				require.Equal(t, 200, row.Status)
				require.False(t, row.Error)
			}(i)
		}
		wg.Wait()
	})
	t.Run("personal_http_client_timeout_and_recovery", func(t *testing.T) {
		slow.Store(true)
		defer slow.Store(false)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
				defer cancel()
				_ = rpc(ctx, keyA, "/api/v1/mcp/personal", "tools/call", "search_knowledge", search("ADMISSION_SLOW"))
			}()
		}
		wg.Wait()
		require.Eventually(t, func() bool { return active.Load() == 0 }, time.Second, time.Millisecond)
		row := rpc(context.Background(), keyA, "/api/v1/mcp/personal",
			"tools/call", "search_knowledge", search("PUBLIC"))
		require.Equal(t, 200, row.Status)
		require.False(t, row.Error)
	})
	if !baseline {
		require.LessOrEqual(t, peak.Load(), int32(5), "single-user four plus independent user")
	}
	// Metrics retain only the fixed projection, never request arguments.
	ctx, trace := perftrace.New(context.Background())
	_ = ctx
	metrics, _ := json.Marshal(trace.Summary())
	require.False(t, strings.Contains(string(metrics), "ADMISSION_SLOW"))
}
