package feishusafety

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type httpEvidenceWire struct{ calls int }

func (w *httpEvidenceWire) RoundTrip(*http.Request) (*http.Response, error) {
	w.calls++
	return &http.Response{
		StatusCode: 500, Header: http.Header{"X-Request-Id": []string{"private-request-id"}},
		Body: io.NopCloser(strings.NewReader("PRIVATE_PROVIDER_BODY")),
	}, nil
}

func TestHTTPStopEvidenceDistinguishesProviderAndPreservesFirstFailure(t *testing.T) {
	for _, family := range []string{"feishu", "embedding", "background_model"} {
		t.Run(family, func(t *testing.T) {
			g, client := fixture(t, Config{})
			InstallDefault(g)
			t.Cleanup(func() { InstallDefault(nil) })
			ctx := WithScope(context.Background(), "private-source-id")
			req, err := http.NewRequestWithContext(ctx, http.MethodPost,
				"https://gateway.invalid/open-apis/drive/v1/export_tasks?token=PRIVATE_QUERY",
				strings.NewReader(`{"input":["PRIVATE_INPUT"]}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer PRIVATE_TOKEN")
			wire := &httpEvidenceWire{}
			transport := Transport{
				Base: wire, Embedding: family == "embedding", BackgroundModel: family == "background_model",
			}
			if _, err = transport.RoundTrip(req); err == nil || g.Check(ctx) == nil {
				t.Fatal("HTTP failure did not close outbound")
			}
			if client.Get(ctx, g.key("trip")).Val() != family+"_http_500" {
				t.Fatal("wrong provider family")
			}
			records, err := client.XRange(ctx, g.key("audit"), "-", "+").Result()
			if err != nil || len(records) != 1 {
				t.Fatal("missing atomic first-failure audit", err)
			}
			raw, ok := records[0].Values["http_evidence"].(string)
			var evidence httpStopEvidence
			if !ok || json.Unmarshal([]byte(raw), &evidence) != nil || evidence.Family != family ||
				evidence.Status != 500 || evidence.SourceHash != httpAuditDigest("private-source-id") ||
				evidence.RequestIDHash != httpAuditDigest("private-request-id") {
				t.Fatal("incomplete failure evidence")
			}
			secrets := []string{"PRIVATE_", "private-request-id", "private-source-id", "gateway.invalid", "https://"}
			for _, secret := range secrets {
				if strings.Contains(raw, secret) {
					t.Fatal("sensitive data entered audit")
				}
			}
			if _, err = transport.RoundTrip(req); err == nil || wire.calls != 1 {
				t.Fatal("stopped transport made another request")
			}
			if err = g.Trip(ctx, "later_failure"); err != nil {
				t.Fatal(err)
			}
			after, err := client.XRange(ctx, g.key("audit"), "-", "+").Result()
			if err != nil || len(after) != 1 || after[0].Values["http_evidence"] != raw ||
				client.Get(ctx, g.key("trip")).Val() != family+"_http_500" {
				t.Fatal("later failure replaced the original evidence")
			}
		})
	}
}
