package embedding

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func TestOllamaCanaryFailsBeforeAnyOutbound(t *testing.T) {
	e := &OllamaEmbedder{} // nil service would panic if either path reached it.
	ctx := feishusafety.WithScope(context.Background(), "isolated-source")
	if _, err := e.Embed(ctx, "sample"); err != feishusafety.ErrClosed {
		t.Fatalf("ungoverned Ollama allowed: %v", err)
	}
	if _, err := e.BatchEmbed(ctx, []string{"sample"}); err != feishusafety.ErrClosed {
		t.Fatalf("ungoverned Ollama batch allowed: %v", err)
	}
}

func TestNewEmbeddingHTTPClient_ReusesTransport(t *testing.T) {
	firstTimeout := 15 * time.Second
	secondTimeout := 45 * time.Second
	first := newEmbeddingHTTPClient(firstTimeout)
	second := newEmbeddingHTTPClient(secondTimeout)

	if first == second {
		t.Fatal("expected distinct HTTP clients")
	}
	firstGuard, ok := first.Transport.(*secutils.SSRFValidatingRoundTripper)
	if !ok {
		t.Fatalf("expected SSRF-validating transport, got %T", first.Transport)
	}
	secondGuard, ok := second.Transport.(*secutils.SSRFValidatingRoundTripper)
	if !ok {
		t.Fatalf("expected SSRF-validating transport, got %T", second.Transport)
	}
	if firstGuard.Base != secondGuard.Base {
		t.Fatal("expected embedding HTTP clients to share a base transport")
	}
	safety, ok := firstGuard.Base.(feishusafety.Transport)
	if !ok || !safety.Embedding {
		t.Fatal("expected embedding safety wrapper inside SSRF validation")
	}
	if safety.Base != http.RoundTripper(sharedEmbeddingHTTPTransport) {
		t.Fatal("expected embedding HTTP client to use the shared transport")
	}
	if first.Timeout != firstTimeout {
		t.Fatalf("unexpected first client timeout: got %v, want %v", first.Timeout, firstTimeout)
	}
	if second.Timeout != secondTimeout {
		t.Fatalf("unexpected second client timeout: got %v, want %v", second.Timeout, secondTimeout)
	}
}

func TestValidateEmbeddingBaseURL_RejectsLoopback(t *testing.T) {
	err := validateEmbeddingBaseURL("http://169.254.169.254/latest/meta-data")
	if err == nil {
		t.Fatal("expected SSRF error for link-local metadata URL")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEmbeddingBaseURL_AllowsEmpty(t *testing.T) {
	if err := validateEmbeddingBaseURL(""); err != nil {
		t.Fatalf("empty base URL should be allowed: %v", err)
	}
}

func TestNewEmbedder_RejectsPrivateBaseURL(t *testing.T) {
	_, err := NewEmbedder(Config{
		Source:    types.ModelSourceRemote,
		Provider:  "openai",
		BaseURL:   "http://169.254.169.254/latest/meta-data",
		ModelName: "text-embedding-3-small",
		APIKey:    "test-key",
	}, nil, nil)
	if err == nil {
		t.Fatal("expected SSRF rejection for link-local metadata URL")
	}
}
