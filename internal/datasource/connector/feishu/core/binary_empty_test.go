package core

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestBinaryEmptyResponseRetriesWithoutAcceptingEmptySource(t *testing.T) {
	for _, tc := range []struct {
		name, operation string
		status          int
	}{
		{"drive", "download_drive_file", http.StatusOK},
		{"export", "download_export_file", http.StatusOK},
		{"media", "download_media_file", http.StatusOK},
		{"no_content", "download_drive_file", http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server, cfg := retryTestServer("/binary", func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
			})
			defer server.Close()
			client := NewClient(cfg)
			client.retry.maxRetries = 2
			var observations []APIObservation
			ctx := WithAPIObserver(t.Context(), func(e APIObservation) {
				if e.Operation == tc.operation {
					observations = append(observations, e)
				}
			})
			data, err := client.downloadRawBytesOperation(ctx, tc.operation, "/binary")
			require.Nil(t, data)
			require.ErrorIs(t, err, ErrTransient)
			require.EqualValues(t, 3, calls.Load())
			require.Len(t, observations, 3)
			for _, e := range observations {
				require.Equal(t, ErrorCategoryTransient, e.Category)
				require.Equal(t, "empty_binary_response", e.ProviderReason)
			}
			meta := FeishuErrorItemMeta(err, nil)
			require.Equal(t, "feishu_empty_download", meta["error_reason_code"])
			require.Equal(t, "transient", meta["feishu_error_category"])
		})
	}
}

func TestBinaryEmptyResponseCanRecoverWithOriginalBytes(t *testing.T) {
	var calls atomic.Int32
	original := []byte("complete-original-source")
	target := "/open-apis/drive/v1/files/file/download"
	server, cfg := retryTestServer(target, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) >= 3 {
			_, _ = w.Write(original)
		}
	})
	defer server.Close()
	client := NewClient(cfg)
	client.retry.maxRetries = 2
	data, err := client.DownloadDriveFile(t.Context(), "file")
	require.NoError(t, err)
	require.Equal(t, original, data)
	require.EqualValues(t, 3, calls.Load())
}

func TestEmptyMetadataResponseStillSucceeds(t *testing.T) {
	var calls atomic.Int32
	server, cfg := retryTestServer("/metadata", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	defer server.Close()
	err := NewClient(cfg).DoRequest(t.Context(), http.MethodDelete, "/metadata", nil, nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
}

type partialTimeoutBody struct{}

func (partialTimeoutBody) Read(p []byte) (int, error) {
	return copy(p, "partial"), context.DeadlineExceeded
}

func TestBinaryPartialReadNeverBecomesEmptyOrSuccessful(t *testing.T) {
	client := NewClient(&Config{AppID: uuid.NewString()})
	client.retry.maxRetries = 0
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header), Request: req,
			ContentLength: 100, Body: io.NopCloser(partialTimeoutBody{}),
		}, nil
	})}
	data, err := client.doRequestBytes(t.Context(), "download_drive_file", http.MethodGet,
		"/binary", nil, false, true, 1024)
	require.Nil(t, data)
	require.ErrorIs(t, err, ErrTransient)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	code, _, _ := feishuFailure(err)
	require.Equal(t, "feishu_timeout", code)
	var api *APIError
	require.ErrorAs(t, err, &api)
	require.Empty(t, api.ProviderReason, "partial transport failures must not be classified as empty responses")
}

func TestEmptyBinaryResponseDoesNotStopIndependentSources(t *testing.T) {
	redisServer := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = rdb.Close(); feishusafety.InstallDefault(nil) })
	cfg := feishusafety.DefaultConfig()
	cfg.Namespace = uuid.NewString()
	guard, err := feishusafety.New(rdb, cfg)
	require.NoError(t, err)
	require.NoError(t, rdb.Set(t.Context(), guard.PermitKey(), guard.Fingerprint(), time.Minute).Err())
	feishusafety.InstallDefault(guard)
	server, config := retryTestServer("/", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/healthy" {
			_, _ = io.WriteString(w, "healthy-original")
		}
	})
	defer server.Close()
	client := NewClient(config)
	client.retry.maxRetries = 1
	_, err = client.doRequestBytes(feishusafety.WithScope(t.Context(), "source-a"),
		"download_drive_file", http.MethodGet, "/empty", nil, false, true, 1024)
	require.ErrorIs(t, err, ErrTransient)
	require.NoError(t, guard.Check(t.Context()))
	data, err := client.doRequestBytes(feishusafety.WithScope(t.Context(), "source-b"),
		"download_drive_file", http.MethodGet, "/healthy", nil, false, true, 1024)
	require.NoError(t, err)
	require.Equal(t, "healthy-original", string(data))
}
