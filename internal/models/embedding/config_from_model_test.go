package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func TestConfigFromModel(t *testing.T) {
	m := &types.Model{
		ID:     "emb-1",
		Name:   "text-embedding-3-small",
		Source: types.ModelSourceRemote,
		Parameters: types.ModelParameters{
			BaseURL:  "https://api.example.com/v1",
			APIKey:   "sk-xxx",
			Provider: "openai",
			EmbeddingParameters: types.EmbeddingParameters{
				Dimension:                 1536,
				TruncatePromptTokens:      512,
				SupportsDimensionOverride: true,
			},
			ExtraConfig:   map[string]string{"region": "us-east"},
			CustomHeaders: map[string]string{"X-Gateway": "g1"},
		},
	}

	cfg := ConfigFromModel(m, "app", "secret")
	if cfg.ModelID != "emb-1" || cfg.ModelName != "text-embedding-3-small" {
		t.Errorf("identity mismatch: %+v", cfg)
	}
	if cfg.Dimensions != 1536 || cfg.TruncatePromptTokens != 512 {
		t.Errorf("embedding params mismatch: %+v", cfg)
	}
	if !cfg.SupportsDimensionOverride {
		t.Errorf("SupportsDimensionOverride not propagated: %+v", cfg)
	}
	if cfg.CustomHeaders["X-Gateway"] != "g1" {
		t.Errorf("CustomHeaders not propagated: %+v", cfg.CustomHeaders)
	}
	if cfg.ExtraConfig["region"] != "us-east" {
		t.Errorf("ExtraConfig not propagated: %+v", cfg.ExtraConfig)
	}
	if cfg.AppID != "app" || cfg.AppSecret != "secret" {
		t.Errorf("cloud creds mismatch: %+v", cfg)
	}
}

func TestSavedModelFactoryPreservesBenchmarkTruncatePromptTokens(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	var request OpenAIEmbedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode embedding request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1,2],"index":0},{"embedding":[3,4],"index":1}]}`))
	}))
	defer server.Close()

	stored := &types.Model{
		ID:     "saved-embedding-v4",
		Name:   "text-embedding-v4",
		Source: types.ModelSourceRemote,
		Parameters: types.ModelParameters{
			BaseURL:  server.URL,
			APIKey:   "server-side-only",
			Provider: "openai",
			EmbeddingParameters: types.EmbeddingParameters{
				Dimension:            2,
				TruncatePromptTokens: 2048,
			},
		},
	}
	embedder, err := NewEmbedder(ConfigFromModel(stored, "", ""), nil, nil)
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	vectors, err := embedder.BatchEmbed(context.Background(), []string{"one", "two"})
	if err != nil {
		t.Fatalf("BatchEmbed: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("vectors = %d, want 2", len(vectors))
	}
	if request.TruncatePromptTokens != 2048 {
		t.Fatalf("truncate_prompt_tokens = %d, want 2048", request.TruncatePromptTokens)
	}
}
