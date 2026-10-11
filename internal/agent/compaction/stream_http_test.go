package compaction

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/Tencent/WeKnora/internal/models/api/openaicompletions"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type a5HTTPLogCapture struct {
	mu   sync.Mutex
	body strings.Builder
}

func (b *a5HTTPLogCapture) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.body.Write(p)
}

func (b *a5HTTPLogCapture) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.body.String()
}

func assertA5SafeErrorChain(t *testing.T, err error, canaries []string) {
	t.Helper()
	for err != nil {
		for _, canary := range canaries {
			assert.NotContains(t, err.Error(), canary)
		}
		err = errors.Unwrap(err)
	}
}

func TestA5HTTPAndSSEErrorsAreSafeAtEveryBoundary(t *testing.T) {
	canaries := []string{"A5_R2_PROVIDER_CANARY", "A5_R2_SQL_CANARY", "A5_R2_TOKEN_CANARY", "Bearer A5_R2_BEARER_CANARY"}
	for _, tc := range []struct {
		name            string
		sse, structured bool
		requests        int32
		code            types.StreamFailureCode
	}{
		{"sse=false", false, false, 2, types.StreamFailureServerUnavailable},
		{"sse=true", true, false, 1, types.StreamFailureUnknown},
		{"structured_sse_temporary", true, true, 2, types.StreamFailureServerUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sse := tc.sse
			capture := &a5HTTPLogCapture{}
			logger.SetOutput(io.MultiWriter(os.Stderr, capture))
			t.Cleanup(func() { logger.SetOutput(os.Stderr) })
			var calls atomic.Int32
			model := a5HTTPModel(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				if sse {
					w.Header().Set("Content-Type", "text/event-stream")
					if tc.structured {
						fmt.Fprintf(w, "data: {\"error\":{\"code\":\"server_error\",\"message\":%q}}\n\n", strings.Join(canaries, " "))
					} else {
						fmt.Fprintf(w, "data: {\"error\":{\"message\":%q}}\n\n", strings.Join(canaries, " "))
					}
				} else {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintf(w, `{"error":{"message":%q}}`, strings.Join(canaries, " "))
				}
			})
			if sse {
				stream, err := model.ChatStream(context.Background(), []chat.Message{{Role: "user", Content: "history"}}, &chat.ChatOptions{})
				require.NoError(t, err)
				errorEvents := 0
				for chunk := range stream {
					if chunk.ResponseType == types.ResponseTypeError {
						errorEvents++
						for _, canary := range canaries {
							assert.NotContains(t, chunk.Content, canary, "provider stream error event must be safe")
						}
					}
				}
				assert.Equal(t, 1, errorEvents)
			}
			before := calls.Load()
			c := New(model, newEstimator(t), testSettings())
			summary, err := c.summarize(context.Background(), []chat.Message{{Role: "user", Content: "history"}}, 0, "", "", 100)
			require.Error(t, err)
			assert.Empty(t, summary)
			assertA5SafeErrorChain(t, err, canaries)
			assert.Equal(t, tc.requests, calls.Load()-before)
			var failure *types.StreamFailure
			require.True(t, errors.As(err, &failure))
			require.Equal(t, tc.code, failure.Code())
			for _, canary := range canaries {
				assert.NotContains(t, capture.String(), canary, "runtime logger must be safe before compactor processing")
			}
		})
	}
}

func TestCompactionHTTPStreamCheckpoint(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failures  int32
		wantCalls int32
		degraded  bool
	}{
		{name: "complete", wantCalls: 2},
		{name: "retry after EOF", failures: 1, wantCalls: 3},
		{name: "persistent EOF", failures: 4, wantCalls: 4, degraded: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SSRF_WHITELIST", "127.0.0.1")
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeSummarySSE(w, calls.Add(1) <= tc.failures)
			}))
			defer server.Close()
			model := openaicompletions.New(openaicompletions.Config{Endpoint: api.Endpoint{
				BaseURL: server.URL, Model: "summary-test", Client: server.Client(),
			}})
			c := New(model, newEstimator(t), testSettings())
			messages := withStoredHistory(reactTurn(12), storedTurn("stored-turn", 1, "/workspace/a.txt"))
			result, err := c.Compact(context.Background(), messages, ReasonThreshold)
			require.NoError(t, err)
			require.NotNil(t, result.Checkpoint)
			assert.Equal(t, tc.wantCalls, calls.Load())
			assert.Equal(t, tc.degraded, result.Degraded)
			assert.Equal(t, tc.degraded, result.Checkpoint.Degraded)
			assert.NotContains(t, result.Summary, "INCOMPLETE_SUMMARY")
			assert.NotContains(t, result.Checkpoint.Summary, "INCOMPLETE_SUMMARY")
			if tc.degraded {
				assert.Contains(t, result.Checkpoint.Summary, "Raw conversation archive")
				assert.Contains(t, result.Checkpoint.Summary, "question stored-turn")
			} else {
				assert.Contains(t, result.Checkpoint.Summary, "COMPLETE_SUMMARY")
			}
		})
	}
}

func a5HTTPModel(t *testing.T, handler http.HandlerFunc) chat.Chat {
	t.Helper()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return openaicompletions.New(openaicompletions.Config{Endpoint: api.Endpoint{
		BaseURL: srv.URL, Model: "summary-test", Client: srv.Client(),
	}})
}

func TestA5HTTPIncompleteClassificationAndBudget(t *testing.T) {
	var calls atomic.Int32
	model := a5HTTPModel(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeSummarySSE(w, true)
	})
	c := New(model, newEstimator(t), testSettings())
	messages := []chat.Message{{Role: "user", Content: "synthetic history"}}
	content, finish, err := c.streamSummary(context.Background(), messages, &chat.ChatOptions{MaxTokens: 100})
	require.NoError(t, err)
	require.Equal(t, "INCOMPLETE_SUMMARY", content)
	require.Equal(t, types.FinishReasonIncomplete, finish)
	require.EqualError(t, validateSummary(content, finish), types.StreamEndedEarlyError)
	before := calls.Load()
	summary, err := c.summarize(context.Background(), messages, 0, "", "", 100)
	require.ErrorContains(t, err, types.StreamEndedEarlyError)
	assert.Empty(t, summary)
	assert.Equal(t, int32(2), calls.Load()-before)
	assert.NotContains(t, err.Error(), "INCOMPLETE_SUMMARY")
}

func TestA5HTTPProviderErrorMustNotLeakCanary(t *testing.T) {
	const canary = "A5_SYNTHETIC_HTTP_PROVIDER_CANARY"
	model := a5HTTPModel(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":{"message":%q,"type":"synthetic_provider_error"}}`, canary)
	})
	c := New(model, newEstimator(t), testSettings())
	summary, err := c.summarize(context.Background(), []chat.Message{{Role: "user", Content: "synthetic history"}}, 0, "", "", 100)
	require.Error(t, err)
	assert.Empty(t, summary)
	assert.NotContains(t, err.Error(), canary, "provider body must not become a visible compaction error")
}

func TestA5HTTPRealCancellationStopsRetries(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprintf("deadline=%v", deadline), func(t *testing.T) {
			started := make(chan struct{})
			var calls atomic.Int32
			model := a5HTTPModel(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					close(started)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			})
			ctx, cancel := context.WithCancel(context.Background())
			if deadline {
				ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
			}
			defer cancel()
			c := New(model, newEstimator(t), testSettings())
			finished := make(chan error, 1)
			go func() {
				_, err := c.summarize(ctx, []chat.Message{{Role: "user", Content: "history"}}, 0, "", "", 100)
				finished <- err
			}()
			select {
			case <-started:
			case <-time.After(10 * time.Second):
				t.Fatal("provider did not start")
			}
			if !deadline {
				cancel()
			}
			select {
			case err := <-finished:
				if deadline {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				} else {
					require.ErrorIs(t, err, context.Canceled)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("cancellation did not finish")
			}
			assert.Equal(t, int32(1), calls.Load(), "a cancelled summary must not retry")
		})
	}
}

// The real provider must classify a clean HTTP EOF
// without finish_reason as incomplete before the compactor decides to commit.
func writeSummarySSE(w http.ResponseWriter, incomplete bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	if incomplete {
		_, _ = fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"INCOMPLETE_SUMMARY"}}]}`+"\n\n")
	} else {
		_, _ = fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"content":"COMPLETE_SUMMARY"},`+
			`"finish_reason":"stop"}]}`+"\n\n")
	}
}
