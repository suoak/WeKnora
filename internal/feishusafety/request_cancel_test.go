package feishusafety

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type cancellationBody struct{ ctx context.Context }

func (b cancellationBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (cancellationBody) Close() error { return nil }

type cancellationTransport struct {
	headers bool
	started chan struct{}
}

func (c cancellationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	close(c.started)
	if !c.headers {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	return &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header),
		Body: cancellationBody{ctx: r.Context()}, Request: r,
	}, nil
}

func TestSafetyCallerCancellationDoesNotTripIndependentSources(t *testing.T) {
	for _, headers := range []bool{false, true} {
		for _, embedding := range []bool{false, true} {
			name := "source"
			if embedding {
				name = "embedding"
			}
			if headers {
				name += "_body"
			} else {
				name += "_headers"
			}
			t.Run(name, func(t *testing.T) {
				g, client := fixture(t, Config{})
				InstallDefault(g)
				ctx, cancel := context.WithCancel(WithScope(context.Background(), "canceled-source"))
				defer cancel()
				req, err := http.NewRequestWithContext(ctx, http.MethodPost,
					"https://gateway.invalid/embeddings", strings.NewReader(`{"input":["test"]}`))
				if err != nil {
					t.Fatal(err)
				}
				started, done := make(chan struct{}), make(chan error, 1)
				go func() {
					transport := Transport{
						Base: cancellationTransport{headers, started}, Embedding: embedding,
					}
					resp, err := transport.RoundTrip(req)
					if err == nil {
						_, err = io.ReadAll(resp.Body)
						_ = resp.Body.Close()
					}
					done <- err
				}()
				<-started
				cancel()
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("caller cancellation lost: %v", err)
					}
				case <-time.After(time.Second):
					t.Fatal("canceled request did not drain")
				}
				if client.Exists(context.Background(), g.key("trip")).Val() != 0 ||
					g.Check(context.Background()) != nil {
					t.Fatal("one canceled source globally tripped independent sources")
				}
				if client.HLen(context.Background(), g.key("inflight")).Val() != 0 {
					t.Fatal("canceled request leaked its shared reservation")
				}
			})
		}
	}
}

func TestSafetyCallerDeadlinePolicy(t *testing.T) {
	for _, headers := range []bool{false, true} {
		for _, embedding := range []bool{false, true} {
			g, client := fixture(t, Config{})
			InstallDefault(g)
			ctx, cancel := context.WithTimeout(
				WithScope(context.Background(), "timed-out-source"), 200*time.Millisecond,
			)
			req, err := http.NewRequestWithContext(
				ctx, http.MethodPost, "https://gateway.invalid/embeddings",
				strings.NewReader(`{"input":["test"]}`),
			)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := (Transport{
				Base: cancellationTransport{headers, make(chan struct{})}, Embedding: embedding,
			}).RoundTrip(req)
			if err == nil {
				_, err = io.ReadAll(resp.Body)
				_ = resp.Body.Close()
			}
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline failure lost: %v", err)
			}
			if embedding {
				if client.Exists(context.Background(), g.key("trip")).Val() != 1 ||
					g.Check(context.Background()) == nil {
					t.Fatal("embedding timeout failed open")
				}
			} else if client.Exists(context.Background(), g.key("trip")).Val() != 0 ||
				g.Check(context.Background()) != nil {
				t.Fatal("source timeout globally stopped independent sources")
			}
		}
	}
}

func TestSafetyCallerCancellationRealHTTP(t *testing.T) {
	g, client := fixture(t, Config{})
	InstallDefault(g)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(WithScope(context.Background(), "source-a"))
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Transport: Transport{Base: server.Client().Transport}, Timeout: time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	_, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("real HTTP did not propagate cancellation: %v", err)
	}
	if g.Check(context.Background()) != nil || client.Exists(context.Background(), g.key("trip")).Val() != 0 {
		t.Fatal("real caller cancellation tripped the cluster")
	}
}

func TestSafetyTruncatedRealHTTPStillTrips(t *testing.T) {
	g, client := fixture(t, Config{})
	InstallDefault(g)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "128")
		_, _ = io.WriteString(w, "short")
	}))
	defer server.Close()
	req, err := http.NewRequestWithContext(
		WithScope(context.Background(), "source-a"), http.MethodGet, server.URL, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (Transport{Base: server.Client().Transport}).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated body was accepted: %v", err)
	}
	if got := client.Get(context.Background(), g.key("trip")).Val(); got != "feishu_response_unexpected_eof" {
		t.Fatalf("truncation did not preserve its closed diagnostic: %q", got)
	}
	if g.Check(context.Background()) == nil {
		t.Fatal("truncated response left outbound enabled")
	}
}

func TestSafetyRequestFailureCategoriesStayClosed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		cancel    bool
		embedding bool
		want      string
	}{
		{"unattributed_cancel", context.Canceled, false, false, "feishu_response_canceled_without_caller"},
		{"truncated_response", io.ErrUnexpectedEOF, false, false, "feishu_response_unexpected_eof"},
		{"embedding_truncated", io.ErrUnexpectedEOF, false, true, "embedding_response_unexpected_eof"},
		{"corruption_after_cancel", io.ErrUnexpectedEOF, true, false, "feishu_response_unexpected_eof"},
		{"deadline_after_cancel", context.DeadlineExceeded, true, true, "embedding_response_timeout"},
		{"other_failure", errors.New("secret response"), false, true, "embedding_response_io_failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, client := fixture(t, Config{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			tripRequestFailure(ctx, g, tc.err, tc.embedding, "response")
			if got := client.Get(context.Background(), g.key("trip")).Val(); got != tc.want {
				t.Fatalf("failure category = %q, want %q", got, tc.want)
			}
			if g.Check(context.Background()) == nil ||
				client.Exists(context.Background(), g.PermitKey()).Val() != 0 {
				t.Fatal("unconfirmed network failure did not close outbound")
			}
		})
	}
}

func TestSafetySourceTimeoutUnconfirmedGateStillTrips(t *testing.T) {
	for _, redisUnavailable := range []bool{false, true} {
		g, client := fixture(t, Config{})
		if redisUnavailable {
			if err := client.Close(); err != nil {
				t.Fatal(err)
			}
		} else if err := client.Del(context.Background(), g.PermitKey()).Err(); err != nil {
			t.Fatal(err)
		}
		tripRequestFailure(context.Background(), g, context.DeadlineExceeded, false, "response")
		if !g.localClosed.Load() || g.Check(context.Background()) == nil {
			t.Fatal("source timeout classification failed open on unconfirmed gate")
		}
		if !redisUnavailable {
			want := "feishu_response_timeout_state_unconfirmed"
			if got := client.Get(context.Background(), g.key("trip")).Val(); got != want {
				t.Fatalf("unconfirmed source timeout reason = %q", got)
			}
		}
	}
}
