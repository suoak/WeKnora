package notion

// CI-only supplemental full-package overlay. All credentials and servers are synthetic.
import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
	"golang.org/x/time/rate"
)

func TestA4CINestedHTTPFailure(t *testing.T) {
	for _, mode := range []string{"429", "500", "cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			if mode == "deadline" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			}
			defer cancel()
			if mode == "cancel" {
				go func() {
					select {
					case <-entered:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/blocks/parent/children" {
					_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"id": "child", "type": "paragraph", "has_children": true}}, "has_more": false})
					return
				}
				select {
				case entered <- struct{}{}:
				default:
				}
				if mode == "cancel" || mode == "deadline" {
					select {
					case <-release:
					case <-r.Context().Done():
					}
					return
				}
				status, _ := strconv.Atoi(mode)
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"message":"TOKEN-CANARY SECRET-CANARY SQL-CANARY"}`)
			}))
			defer srv.Close()
			defer close(release)
			client := &notionClient{baseURL: srv.URL, httpClient: srv.Client(), limiter: rate.NewLimiter(rate.Inf, 1)}
			blocks, err := client.GetBlockChildrenAll(ctx, "parent")
			if err == nil || blocks != nil || errors.Is(err, datasource.ErrResourceNotFound) {
				t.Fatal("failed child became complete parent or missing resource")
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation classification lost")
			}
			if mode == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("deadline classification lost")
			}
			t.Logf("nested mode=%s parent=200 content=nil error=true", mode)
		})
	}
}
