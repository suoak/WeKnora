package feishusafety

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type embeddingTimeoutRoundTripFunc func(*http.Request) (*http.Response, error)

func (f embeddingTimeoutRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestEmbeddingHeaderTimeoutIsLocalOnlyWithHealthyRuntime(t *testing.T) {
	for _, lostPermit := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost_permit=%t", lostPermit), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if r.URL.Path == "/slow" {
					<-r.Context().Done()
					return
				}
				_, _ = io.WriteString(w, `{"data":[{"embedding":[1]}]}`)
			}))
			defer srv.Close()
			g, client := fixture(t, Config{EndpointHost: strings.TrimPrefix(srv.URL, "http://")})
			InstallDefault(g)
			wire := http.DefaultTransport
			base := embeddingTimeoutRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if lostPermit {
					if err := client.Del(context.Background(), g.PermitKey()).Err(); err != nil {
						return nil, err
					}
				}
				return wire.RoundTrip(req)
			})
			httpClient := &http.Client{
				Timeout:   100 * time.Millisecond,
				Transport: Transport{Base: base, Embedding: true},
			}
			ctx := WithScope(context.Background(), "affected-source")
			req, err := http.NewRequestWithContext(
				ctx,
				http.MethodPost,
				srv.URL+"/slow",
				strings.NewReader(`{"input":["test"]}`),
			)
			if err != nil {
				t.Fatal(err)
			}
			response, err := httpClient.Do(req)
			if response != nil || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout returned a response or lost its cause: %v", err)
			}
			tripped := client.Exists(context.Background(), g.key("trip")).Val() != 0
			if tripped != lostPermit {
				t.Fatalf("wrong shared stop after local request drained: lostPermit=%t", lostPermit)
			}
			if client.HLen(context.Background(), g.key("inflight")).Val() != 0 ||
				client.ZCard(context.Background(), g.key("window")).Val() != 1 {
				t.Fatal("timeout leaked a slot or refunded its wire charge")
			}
			if !lostPermit {
				healthy := WithScope(context.Background(), "independent-source")
				next, err := http.NewRequestWithContext(
					healthy,
					http.MethodPost,
					srv.URL+"/healthy",
					strings.NewReader(`{"input":["test"]}`),
				)
				if err != nil {
					t.Fatal(err)
				}
				response, err := httpClient.Do(next)
				if err != nil {
					t.Fatalf("independent source stopped: %v", err)
				}
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
			}
		})
	}
}
