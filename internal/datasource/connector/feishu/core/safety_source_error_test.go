package core

import (
	"context"
	"errors"
	"fmt"
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

func TestSourceHTTP400401KeepCoreDiagnosisAndOtherSourcesLive(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := miniredis.RunT(t)
			redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = redisClient.Close(); feishusafety.InstallDefault(nil) })
			policy := feishusafety.DefaultConfig()
			policy.Namespace = uuid.NewString()
			guard, err := feishusafety.New(redisClient, policy)
			if err != nil {
				t.Fatal(err)
			}
			ctx := feishusafety.WithScope(context.Background(), "source-a")
			if err = redisClient.Set(ctx, guard.PermitKey(), guard.Fingerprint(), time.Minute).Err(); err != nil {
				t.Fatal(err)
			}
			feishusafety.InstallDefault(guard)
			client := NewClient(&Config{AppID: uuid.NewString(), BaseURL: "https://open.feishu.cn"})
			base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				code, body := status, `{"code":99991663,"msg":"invalid existing source identity"}`
				if request.URL.Path == "/other" {
					code, body = http.StatusOK, `{"code":0,"data":{}}`
				}
				return &http.Response{
					StatusCode: code, Header: make(http.Header),
					Body: io.NopCloser(strings.NewReader(body)), Request: request,
				}, nil
			})
			client.httpClient = &http.Client{Transport: feishusafety.Transport{Base: base}}
			_, err = client.doRequestBytes(ctx, "source_read", http.MethodGet, "/source", nil, false, false, 0)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.HTTPStatus != status || apiErr.Code != 99991663 {
				t.Fatalf("lost source HTTP/code diagnosis: %v", err)
			}
			if err = guard.Check(ctx); err != nil {
				t.Fatal("source error globally tripped", err)
			}
			otherCtx := feishusafety.WithScope(context.Background(), "source-b")
			_, err = client.doRequestBytes(
				otherCtx, "other_source_read", http.MethodGet, "/other", nil, false, false, 0,
			)
			if err != nil {
				t.Fatal(fmt.Errorf("other source blocked: %w", err))
			}
		})
	}
}
