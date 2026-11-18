package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderMessageEvidenceDoesNotChangeDenialOrIdentity(t *testing.T) {
	for _, tc := range []struct{ name, message, want string }{
		{"safe", "no permission", "no permission"},
		{"credential", "no permission; access_token=private-value", ""},
		{"url", "no permission https://private.test/docx/document", ""},
		{"unreviewed", "provider echoed arbitrary document content", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer original-user" {
					t.Error("export denial changed identity")
				}
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 1069902, "msg": tc.message})
			}))
			defer server.Close()
			var event APIObservation
			ctx := WithAPIObserver(context.Background(), func(value APIObservation) { event = value })
			ctx = WithUserIdentity(ctx, func(call context.Context) (string, error) {
				if UserTokenRefreshFingerprint(call) != "" {
					t.Error("export denial triggered token refresh")
				}
				return "original-user", nil
			})
			err := NewClient(&Config{BaseURL: server.URL}).DoRequest(ctx, http.MethodGet, "/target", nil, nil)
			if !errors.Is(err, ErrPermission) || calls != 1 || event.Code != 1069902 ||
				event.ProviderMessage != tc.want {
				t.Fatalf("unexpected export refusal or message evidence: calls=%d, code=%d", calls, event.Code)
			}
			encoded, _ := json.Marshal(event)
			for _, secret := range []string{"original-user", "private-value", "private.test", "document content"} {
				if strings.Contains(string(encoded), secret) || strings.Contains(err.Error(), secret) {
					t.Fatal("provider text leaked credential, URL or content")
				}
			}
		})
	}
}
