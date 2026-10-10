package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

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

func TestA4FeishuProviderMessagesAreNotPublicOrLogged(t *testing.T) {
	const canary = "TOKEN-CANARY SECRET-CANARY SQL-CANARY Authorization: Bearer SYNTHETIC"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 99991672, "msg": canary})
	}))
	defer srv.Close()
	var logs bytes.Buffer
	logger.SetOutput(&logs)
	defer logger.SetOutput(os.Stderr)
	c := &Client{baseURL: srv.URL, httpClient: srv.Client()}
	_, err := c.doRequestBytes(context.Background(), "synthetic_operation", http.MethodGet, "/failure", nil, false, false, 0)
	if err == nil {
		t.Fatal("provider failure accepted")
	}
	for _, marker := range strings.Fields(canary) {
		if strings.Contains(err.Error(), marker) || strings.Contains(logs.String(), marker) {
			t.Fatal("provider error leaked")
		}
	}
}

func TestA4FeishuLoopbackChunkedOverflowAndCancellation(t *testing.T) {
	for _, mode := range []string{"chunked", "cancel", "deadline", "interrupted"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{}, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				started <- struct{}{}
				switch mode {
				case "chunked":
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
					_, _ = io.WriteString(w, strings.Repeat("x", int(maxJSONResponseBytes+1)))
				case "interrupted":
					conn, rw, _ := w.(http.Hijacker).Hijack()
					_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\npartial")
					_ = rw.Flush()
					_ = conn.Close()
				default:
					<-r.Context().Done()
				}
			}))
			defer srv.Close()
			rp := defaultRetryPolicy()
			rp.maxRetries = 0
			c := &Client{baseURL: srv.URL, httpClient: srv.Client(), retry: rp}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "deadline" {
				ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
				defer cancel()
			}
			if mode == "cancel" {
				go func() { <-started; cancel() }()
			}
			data, err := c.doRequestBytes(ctx, "synthetic_loopback", http.MethodGet, "/test", nil, false, false, 0)
			if data != nil || err == nil {
				t.Fatal("failed response returned usable data")
			}
			switch mode {
			case "chunked":
				if !errors.Is(err, ErrResponseTooLarge) {
					t.Fatal("chunked bypassed cap")
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancel classification lost")
				}
			case "deadline":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("deadline classification lost")
				}
			case "interrupted":
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatal("read failure classification lost")
				}
			}
		})
	}
}
