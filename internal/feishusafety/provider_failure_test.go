package feishusafety

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNormalProviderHTTPFailureKeepsSiblingAdmissionAndAccounting(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 408, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			g, r := fixture(t, Config{})
			InstallDefault(g)
			g.SetProviderFailureIsolation(true)
			ctx := WithScope(context.Background(), "source-a")
			req, _ := http.NewRequestWithContext(ctx, "POST", "https://gateway.invalid/v1/embeddings?token=PRIVATE_QUERY", strings.NewReader(`{"input":["PRIVATE_INPUT"]}`))
			req.Header.Set("Authorization", "Bearer PRIVATE_TOKEN")
			wire := &httpEvidenceWire{status: status}
			resp, err := (Transport{Base: wire, Embedding: true}).RoundTrip(req)
			if err != nil || resp == nil || resp.StatusCode != status {
				t.Fatalf("lost provider status: %v", err)
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil || string(body) != "PRIVATE_PROVIDER_BODY" {
				t.Fatal("SDK cannot inspect provider response", err)
			}
			EmbeddingResultFailed(ctx, "embedding_response_invalid_or_business_error")
			if r.Exists(ctx, g.key("trip")).Val() != 0 || g.Check(WithScope(context.Background(), "source-b")) != nil {
				t.Fatal("provider failure latched shared service")
			}
			if r.HLen(ctx, g.key("inflight")).Val() != 0 || r.ZCard(ctx, g.key("window")).Val() != 1 {
				t.Fatal("slot leaked or wire charge refunded")
			}
			entries, err := r.XRange(ctx, g.key("audit"), "-", "+").Result()
			if err != nil || len(entries) != 2 {
				t.Fatal("missing local failure audit", err)
			}
			for _, entry := range entries {
				for _, value := range entry.Values {
					if strings.Contains(value.(string), "PRIVATE_") {
						t.Fatal("private data in audit")
					}
				}
			}
			next, _ := http.NewRequestWithContext(WithScope(context.Background(), "source-b"), "POST", "https://gateway.invalid/v1/embeddings", strings.NewReader(`{"input":["next"]}`))
			wire.status = 200
			resp, err = (Transport{Base: wire, Embedding: true}).RoundTrip(next)
			if err != nil || resp.StatusCode != 200 {
				t.Fatal("sibling denied", err)
			}
			_ = resp.Body.Close()
			if err := g.Trip(ctx, "operator_emergency_stop"); err != nil {
				t.Fatal(err)
			}
			_, err = (Transport{Base: wire, Embedding: true}).RoundTrip(next)
			if err == nil || wire.calls != 2 {
				t.Fatal("operator stop bypassed")
			}
		})
	}
}

func TestNormalEmbeddingRetryExhaustionIsFiniteAndDoesNotLatch(t *testing.T) {
	g, r := fixture(t, Config{})
	InstallDefault(g)
	g.SetProviderFailureIsolation(true)
	ctx := WithScope(context.Background(), "rate-limited")
	var waited time.Duration
	if err := WaitEmbeddingRetry(ctx, "", 2, &waited); !errors.Is(err, ErrProviderFailure) {
		t.Fatal("missing finite provider failure", err)
	}
	if r.Exists(ctx, g.key("trip")).Val() != 0 || g.Check(ctx) != nil {
		t.Fatal("429 exhaustion stopped service")
	}
}
