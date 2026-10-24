package core

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExportRangeDownloadAssemblyAndFiniteRetry(t *testing.T) {
	for _, mode := range []string{"multipart", "whole_response", "retry_429", "retry_exhausted"} {
		t.Run(mode, func(t *testing.T) {
			payload := []byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
			var mu sync.Mutex
			var ranges []string
			var middleAttempts int
			server, cfg := retryTestServer("/target", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				ranges = append(ranges, r.Header.Get("Range"))
				require.Equal(t, "identity", r.Header.Get("Accept-Encoding"))
				if mode == "whole_response" {
					_, _ = w.Write(payload)
					return
				}
				var start, end int
				_, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
				require.NoError(t, err)
				if start == 20 {
					middleAttempts++
					if mode == "retry_exhausted" || (mode == "retry_429" && middleAttempts == 1) {
						w.Header().Set("Retry-After", "0")
						w.WriteHeader(http.StatusTooManyRequests)
						_, _ = w.Write([]byte(`{"code":99991400}`))
						return
					}
				}
				end = min(end, len(payload)-1)
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
				w.Header().Set("ETag", `"fixed-export-file"`)
				if start > 0 {
					require.Equal(t, `"fixed-export-file"`, r.Header.Get("If-Match"))
				}
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(payload[start : end+1])
			})
			defer server.Close()
			client := fastRetryClient(cfg, 1)
			data, err := client.downloadExportRanges(context.Background(), "/target", 20)
			mu.Lock()
			defer mu.Unlock()
			if mode == "retry_exhausted" {
				require.Error(t, err)
				require.Nil(t, data, "no incomplete export can reach parser or publication")
				require.Equal(t, 2, middleAttempts)
				require.NotContains(t, ranges, "bytes=40-59")
				return
			}
			require.NoError(t, err)
			require.Equal(t, payload, data)
			if mode == "whole_response" {
				require.Len(t, ranges, 1)
			} else {
				require.Equal(t, "bytes=0-19", ranges[0])
				require.Equal(t, "bytes=60-61", ranges[len(ranges)-1])
			}
		})
	}
}

func TestExportRangeDownloadRejectsUnconfirmedAssembly(t *testing.T) {
	for _, mode := range []string{
		"wrong_offset", "changed_total", "changed_etag", "missing_range", "short_body", "oversize_part",
		"whole_after_partial", "encoded_partial", "over_limit_total",
	} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			calls := 0
			server, cfg := retryTestServer("/target", func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				calls++
				start, end, total := 0, 19, 40
				if calls > 1 {
					start, end = 20, 39
				}
				if mode == "wrong_offset" && calls > 1 {
					start, end = 0, 19
				}
				if mode == "changed_total" && calls > 1 {
					total = 41
				}
				if mode == "over_limit_total" {
					total = int(maxFeishuDownloadBytes) + 1
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
				w.Header().Set("ETag", `"fixed"`)
				if mode == "changed_etag" && calls > 1 {
					w.Header().Set("ETag", `"another"`)
				}
				if mode == "missing_range" {
					w.Header().Del("Content-Range")
				}
				if mode == "encoded_partial" {
					w.Header().Set("Content-Encoding", "gzip")
				}
				status := http.StatusPartialContent
				if mode == "whole_after_partial" && calls > 1 {
					status = http.StatusOK
				}
				w.WriteHeader(status)
				body := strings.Repeat("x", 20)
				if mode == "short_body" {
					body = body[:19]
				}
				if mode == "oversize_part" {
					body += "x"
				}
				_, _ = w.Write([]byte(body))
			})
			defer server.Close()
			client := fastRetryClient(cfg, 0)
			data, err := client.downloadExportRanges(context.Background(), "/target", 20)
			mu.Lock()
			defer mu.Unlock()
			require.Error(t, err)
			require.Nil(t, data)
			require.LessOrEqual(t, calls, 2)
		})
	}
}

func TestExportRangeDownloadCancellationKeepsPartialPrivate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	calls := 0
	server, cfg := retryTestServer("/target", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		w.Header().Set("Content-Range", "bytes 0-19/40")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte(strings.Repeat("x", 20)))
		cancel()
	})
	defer server.Close()
	client := fastRetryClient(cfg, 1)
	data, err := client.downloadExportRanges(ctx, "/target", 20)
	mu.Lock()
	defer mu.Unlock()
	require.Error(t, err)
	require.Nil(t, data)
	require.Equal(t, 1, calls)
}
