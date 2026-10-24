package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/stretchr/testify/require"
)

func TestCandidateModelTransportClosedAndOnlineUnaffected(t *testing.T) {
	feishusafety.InstallDefault(nil)
	t.Cleanup(func() { feishusafety.InstallDefault(nil) })
	var sent atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	// Test the actual shared wrapper beneath the independent SSRF validator.
	client := &http.Client{Transport: feishusafety.Transport{Base: http.DefaultTransport, BackgroundModel: true}}
	ctx := feishusafety.WithScope(context.Background(), "source")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	_, err = client.Do(req)
	require.ErrorIs(t, err, feishusafety.ErrClosed)
	require.Zero(t, sent.Load())
	req, err = http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, int32(1), sent.Load())
}
