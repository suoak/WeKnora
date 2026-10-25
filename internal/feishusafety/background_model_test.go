package feishusafety

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSafetyCandidateModelSharesRequestBudgetAndStops(t *testing.T) {
	g, client := fixture(t, Config{InFlight: 1, RPM: 10, RPS: 10})
	InstallDefault(g)
	var calls atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	defer close(release)
	ctx := WithScope(context.Background(), "candidate-source")
	done := make(chan error, 1)
	go func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
		resp, err := (&http.Client{Transport: Transport{BackgroundModel: true}}).Do(req)
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("candidate model did not start")
	}
	require.Equal(t, int64(1), client.HLen(ctx, g.key("inflight")).Val())
	_, err := g.tryBegin(ctx, 1)
	require.Error(t, err, "embedding and candidate model share one request slot")
	require.NoError(t, EmergencyBlock(ctx, client, g.config.Namespace))
	select {
	case err = <-done:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("candidate model did not stop")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
	_, err = (&http.Client{Transport: Transport{BackgroundModel: true}}).Do(req)
	require.Error(t, err)
	require.Equal(t, int32(1), calls.Load())
}
