package core

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestBinaryDownloadContinuesPastMetadataDeadline(t *testing.T) {
	server, cfg := retryTestServer("/binary", func(w http.ResponseWriter, r *http.Request) {
		for range 8 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
			if _, err := w.Write([]byte("source")); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	})
	defer server.Close()
	client := NewClient(cfg)
	client.httpClient.Timeout = 50 * time.Millisecond
	client.downloadTimeout = 2 * time.Second
	client.downloadIdleTimeout = 500 * time.Millisecond
	client.retry.maxRetries = 0
	data, err := client.doRequestBytes(t.Context(), "download_drive_file", http.MethodGet,
		"/binary", nil, false, true, 1024)
	require.NoError(t, err)
	require.Equal(t, "sourcesourcesourcesourcesourcesourcesourcesource", string(data))
	require.Equal(t, 50*time.Millisecond, client.httpClient.Timeout)
	_, err = client.doRequestBytes(t.Context(), "metadata", http.MethodGet, "/binary", nil, false, false, 0)
	require.Error(t, err, "metadata must keep the shorter shared client budget")
}

func TestBinaryDownloadIdleDeadlineRefusesHeadersAndPartialBody(t *testing.T) {
	for _, body := range []bool{false, true} {
		name := "headers"
		if body {
			name = "partial_body"
		}
		t.Run(name, func(t *testing.T) {
			server, cfg := retryTestServer("/binary", func(w http.ResponseWriter, r *http.Request) {
				if body {
					_, _ = w.Write([]byte("incomplete"))
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			})
			defer server.Close()
			client := NewClient(cfg)
			client.downloadTimeout = 2 * time.Second
			client.downloadIdleTimeout = 50 * time.Millisecond
			client.retry.maxRetries = 0
			data, err := client.doRequestBytes(t.Context(), "download_drive_file", http.MethodGet,
				"/binary", nil, false, true, 1024)
			require.Nil(t, data)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.ErrorIs(t, err, ErrTransient)
			require.Equal(t, "feishu_timeout", FeishuErrorItemMeta(err, nil)["error_reason_code"])
		})
	}
}

func TestBinaryDownloadTotalBudgetCannotBeExtendedByProgress(t *testing.T) {
	server, cfg := retryTestServer("/binary", func(w http.ResponseWriter, r *http.Request) {
		for {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
			if _, err := w.Write([]byte("x")); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	})
	defer server.Close()
	client := NewClient(cfg)
	client.downloadTimeout = 100 * time.Millisecond
	client.downloadIdleTimeout = time.Second
	data, err := client.doRequestBytes(t.Context(), "download_drive_file", http.MethodGet,
		"/binary", nil, false, true, 1024)
	require.Nil(t, data)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, "transient", FeishuErrorItemMeta(err, nil)["feishu_error_category"])
}

func TestBinaryDownloadPreservesCallerCancellation(t *testing.T) {
	started := make(chan struct{})
	server, cfg := retryTestServer("/binary", func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	defer server.Close()
	client := NewClient(cfg)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { <-started; cancel() }()
	data, err := client.doRequestBytes(ctx, "download_drive_file", http.MethodGet,
		"/binary", nil, false, true, 1024)
	require.Nil(t, data)
	require.ErrorIs(t, err, context.Canceled)
}

func TestBinaryIdleDeadlineKeepsIndependentSourceHealthy(t *testing.T) {
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = rdb.Close(); feishusafety.InstallDefault(nil) })
	guardConfig := feishusafety.DefaultConfig()
	guardConfig.Namespace = uuid.NewString()
	guard, err := feishusafety.New(rdb, guardConfig)
	require.NoError(t, err)
	require.NoError(t, rdb.Set(t.Context(), guard.PermitKey(), guard.Fingerprint(), time.Minute).Err())
	feishusafety.InstallDefault(guard)
	server, cfg := retryTestServer("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthy" {
			_, _ = w.Write([]byte("complete-source"))
			return
		}
		_, _ = w.Write([]byte("incomplete"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	defer server.Close()
	client := NewClient(cfg)
	client.downloadIdleTimeout = 100 * time.Millisecond
	client.retry.maxRetries = 0
	data, err := client.doRequestBytes(feishusafety.WithScope(t.Context(), "source-a"),
		"download_drive_file", http.MethodGet, "/stalled", nil, false, true, 1024)
	require.Nil(t, data)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, guard.Check(t.Context()))
	data, err = client.doRequestBytes(feishusafety.WithScope(t.Context(), "source-b"),
		"download_drive_file", http.MethodGet, "/healthy", nil, false, true, 1024)
	require.NoError(t, err)
	require.Equal(t, "complete-source", string(data))
}
