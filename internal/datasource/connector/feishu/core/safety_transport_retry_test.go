package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestSourceHeaderDisconnectUsesExistingBoundedRetry(t *testing.T) {
	sourceFailureUsesExistingBoundedRetry(t, nil, false)
}

func TestSourceBodyFailureUsesExistingBoundedRetry(t *testing.T) {
	for _, cause := range []error{io.ErrUnexpectedEOF, errors.New("http2: stream error")} {
		t.Run(TransportErrorCategory(cause), func(t *testing.T) {
			sourceFailureUsesExistingBoundedRetry(t, cause, false)
		})
	}
}

type failedSourceResponse struct {
	cause error
	data  string
}

func (r failedSourceResponse) Read(p []byte) (int, error) {
	// Even apparently valid JSON is not accepted if its transport did not finish.
	return copy(p, r.data), r.cause
}

func (failedSourceResponse) Close() error { return nil }

func TestSourceRangeBodyFailureRetriesWholePartWithoutPartialSuccess(t *testing.T) {
	sourceFailureUsesExistingBoundedRetry(t, errors.New("http2: stream error"), true)
}

func sourceFailureUsesExistingBoundedRetry(t *testing.T, bodyFailure error, rangeDownload bool) {
	t.Helper()
	for _, recoverAfterFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "exhausted", true: "recovered"}[recoverAfterFirst], func(t *testing.T) {
			store := miniredis.RunT(t)
			rc := redis.NewClient(&redis.Options{Addr: store.Addr(), MaxRetries: -1})
			t.Cleanup(func() { _ = rc.Close(); feishusafety.InstallDefault(nil) })
			guard, err := feishusafety.New(
				rc,
				feishusafety.Config{
					Namespace:          "retry-test-" + uuid.NewString(),
					EndpointHost:       "gateway.invalid",
					InFlight:           1,
					RPM:                10,
					RPS:                2,
					TPM:                10000,
					WaitBudgetDuration: time.Second,
					TokenBoundVerified: true,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			err = rc.Set(context.Background(), guard.PermitKey(), guard.Fingerprint(), 10*time.Second).Err()
			if err != nil {
				t.Fatal(err)
			}
			feishusafety.InstallDefault(guard)
			cfg := &Config{BaseURL: "https://source.invalid", AppID: "synthetic-app"}
			client := fastRetryClient(cfg, 2)
			attempts := 0
			client.httpClient.Transport = feishusafety.Transport{
				Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					attempts++
					status, payload, headers := http.StatusOK, `{"code":0}`, make(http.Header)
					if rangeDownload {
						if req.Header.Get("Range") != "bytes=0-1023" {
							t.Fatal("failed part advanced the verified download offset")
						}
						status, payload = http.StatusPartialContent, "full-file"
						headers.Set("Content-Range", "bytes 0-8/9")
						headers.Set("ETag", `"stable-source"`)
					}
					var body io.ReadCloser = io.NopCloser(strings.NewReader(payload))
					if !recoverAfterFirst || attempts == 1 {
						if bodyFailure != nil {
							body = failedSourceResponse{cause: bodyFailure, data: payload}
						} else {
							return nil, io.EOF
						}
					}
					return &http.Response{
						StatusCode:    status,
						Header:        headers,
						Body:          body,
						ContentLength: int64(len(payload)),
					}, nil
				}),
			}
			ctx := WithUserIdentity(
				feishusafety.WithScope(context.Background(), "synthetic-source"),
				func(context.Context) (string, error) { return "synthetic-personal-token", nil },
			)
			if rangeDownload {
				var data []byte
				data, err = client.downloadExportRanges(ctx, "/file", 1024)
				if (recoverAfterFirst && string(data) != "full-file") || (!recoverAfterFirst && data != nil) {
					t.Fatal("incomplete or duplicate bytes escaped the bounded download")
				}
			} else {
				err = client.DoRequest(ctx, http.MethodGet, "/metadata", nil, nil)
			}
			if recoverAfterFirst {
				if err != nil || attempts != 2 {
					t.Fatalf("recover: attempts=%d error=%v", attempts, err)
				}
			} else if !errors.Is(err, ErrTransient) || attempts != 3 {
				t.Fatalf("bounded exhaustion: attempts=%d error=%v", attempts, err)
			}
			if guard.Check(context.Background()) != nil {
				t.Fatal("single-source network failure disabled independent sources")
			}
		})
	}
}
