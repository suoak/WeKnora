package confluence

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func builtinTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CONFLUENCE_BUILTIN_ENABLED", "true")
	t.Setenv("CONFLUENCE_BUILTIN_BASE_URL", "http://confluence.internal/wiki")
	t.Setenv("CONFLUENCE_BUILTIN_USERNAME", "system-user-sentinel")
	t.Setenv("CONFLUENCE_BUILTIN_PASSWORD", "system-password-sentinel")
	t.Setenv("CONFLUENCE_BUILTIN_PASSWORD_FILE", "")
}

func TestBuiltinConfluenceLocksDestinationAndCredentials(t *testing.T) {
	builtinTestEnv(t)
	ds := &types.DataSourceConfig{
		Settings:    map[string]interface{}{"credential_source": "system", "base_url": "http://attacker.test", "edition": "cloud", "username": "attacker"},
		Credentials: map[string]interface{}{"base_url": "http://attacker.test", "password": "attacker", "api_token": "attacker", "username": "attacker"},
	}
	cfg, err := parseConfig(ds)
	if err != nil || cfg.baseURL != "http://confluence.internal/wiki" || cfg.username != "system-user-sentinel" || cfg.secret != "system-password-sentinel" || !cfg.systemAccount || cfg.cloud() {
		t.Fatalf("system profile was overridden (err=%v)", err)
	}
	body, err := json.Marshal(BuiltinProfileInfo())
	if err != nil || strings.Contains(string(body), cfg.username) || strings.Contains(string(body), cfg.secret) {
		t.Fatal("public metadata leaked credentials")
	}
	ds.StripNonSecretCredentials(types.ConnectorTypeConfluence)
	blob, err := ds.ToJSON()
	if err != nil || strings.Contains(string(blob), "attacker") || strings.Contains(string(blob), cfg.secret) || strings.Contains(string(blob), cfg.username) || len(ds.Credentials) != 0 {
		t.Fatal("system credentials or overrides persisted")
	}
}

func TestBuiltinConfluenceUnavailableNeverFallsBack(t *testing.T) {
	builtinTestEnv(t)
	t.Setenv("CONFLUENCE_BUILTIN_ENABLED", "false")
	ds := streamConfig()
	ds.Settings = map[string]interface{}{"credential_source": "system"}
	if _, err := parseConfig(ds); err == nil {
		t.Fatal("disabled profile fell back to user credentials")
	}
	if BuiltinProfileInfo().Enabled {
		t.Fatal("disabled system account was advertised")
	}
}

func TestBuiltinConfluenceRawReferenceCanonicalizesWithoutCredentials(t *testing.T) {
	builtinTestEnv(t)
	ds := &types.DataSourceConfig{Credentials: map[string]interface{}{"credential_source": "system", "username": "override", "password": "override", "base_url": "http://attacker.test"}}
	ds.StripNonSecretCredentials(types.ConnectorTypeConfluence)
	if !ds.UsesSystemConfluence() || !ds.HasConfiguredCredentials(types.ConnectorTypeConfluence) || len(ds.Credentials) != 0 {
		t.Fatal("raw system reference was not canonicalized")
	}
	cfg, err := parseConfig(ds)
	if err != nil || !cfg.systemAccount || cfg.baseURL != "http://confluence.internal/wiki" {
		t.Fatalf("raw reference did not resolve the system profile: %v", err)
	}
}

func TestBuiltinConfluenceSecretFileAndSafeErrors(t *testing.T) {
	builtinTestEnv(t)
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("file-password-sentinel\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFLUENCE_BUILTIN_PASSWORD_FILE", path)
	cfg, err := builtinConfig()
	if err != nil || cfg.secret != "file-password-sentinel" {
		t.Fatal("secret file was not used")
	}
	t.Setenv("CONFLUENCE_BUILTIN_PASSWORD_FILE", path+"-missing")
	_, err = builtinConfig()
	if err == nil || strings.Contains(err.Error(), path) || strings.Contains(err.Error(), "sentinel") {
		t.Fatal("missing secret file was ignored or leaked details")
	}
}

func TestBuiltinConfluenceHidesUpstreamAuthResponse(t *testing.T) {
	builtinTestEnv(t)
	client := &client{cfg: config{baseURL: "http://confluence.internal", systemAccount: true}, http: &http.Client{Transport: roundTripper(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("system-user-sentinel system-password-sentinel")), Header: http.Header{}}, nil
	})}}
	err := client.get(context.Background(), "/rest/api/space?limit=1", nil)
	if err == nil || strings.Contains(err.Error(), "sentinel") {
		t.Fatal("upstream auth failure exposed system identity")
	}
}

func TestSystemAccountRedirectStaysOnConfiguredOrigin(t *testing.T) {
	policy := systemAccountRedirectPolicy("http://confluence.internal/wiki", nil)
	for _, target := range []string{"http://evil.confluence.internal/rest/api/space", "http://attacker.test/", "https://confluence.internal/rest/api/space"} {
		req, _ := http.NewRequest(http.MethodGet, target, nil)
		if policy(req, nil) == nil {
			t.Fatalf("accepted off-origin redirect: %s", target)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "http://confluence.internal/wiki/rest/api/space", nil)
	if err := policy(req, nil); err != nil {
		t.Fatal(err)
	}
}
