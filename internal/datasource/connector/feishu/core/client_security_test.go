package core

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/logger"
)

func TestGetTenantAccessTokenDoesNotLogCredentials(t *testing.T) {
	const appSecret = "app-secret-must-not-appear"
	const tenantToken = "tenant-token-must-not-appear"
	receivedSecret := make(chan string, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		receivedSecret <- payload["app_secret"]
		_ = json.NewEncoder(w).Encode(TokenResponse{
			ApiResponse:       ApiResponse{Code: 0},
			TenantAccessToken: tenantToken,
			Expire:            7200,
		})
	}))
	defer server.Close()

	var logs bytes.Buffer
	logger.SetOutput(&logs)
	defer logger.SetOutput(os.Stderr)

	client := &Client{
		baseURL:    server.URL,
		appID:      "app-id",
		appSecret:  appSecret,
		httpClient: server.Client(),
	}
	if _, err := client.GetTenantAccessToken(context.Background()); err != nil {
		t.Fatalf("GetTenantAccessToken: %v", err)
	}
	if got := <-receivedSecret; got != appSecret {
		t.Fatalf("auth request app_secret = %q", got)
	}

	output := logs.String()
	for _, secretFragment := range []string{
		appSecret,
		tenantToken,
		"tenant-t",
		"pear",
		"app_secret",
		"tenant_access_token",
		"Authorization",
		"Bearer ",
	} {
		if strings.Contains(output, secretFragment) {
			t.Fatalf("credential fragment %q leaked in log %q", secretFragment, output)
		}
	}
}
