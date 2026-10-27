package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPersonalRejectedAccessGenerationRefreshesOnce(t *testing.T) {
	for _, rotated := range []bool{true, false} {
		t.Run(fmt.Sprint(rotated), func(t *testing.T) {
			calls, hints, tenantCalls := 0, 0, 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "tenant_access_token") {
					tenantCalls++
					t.Fatal("personal request used application identity")
				}
				calls++
				w.Header().Set("X-Tt-Logid", "request-opaque-123")
				if r.Header.Get("Authorization") == "Bearer new-personal" {
					_, _ = io.WriteString(w, `{"code":0}`)
					return
				}
				_, _ = io.WriteString(w, `{"code":99991668,"msg":"expired"}`)
			}))
			defer ts.Close()
			var events []APIObservation
			currentToken := "old-personal"
			ctx := WithAPIObserver(
				context.Background(),
				func(e APIObservation) { events = append(events, e) },
			)
			ctx = WithUserIdentity(ctx, func(ctx context.Context) (string, error) {
				if UserTokenRefreshFingerprint(ctx) != "" {
					hints++
					if UserTokenRefreshFingerprint(ctx) != tokenFingerprint("old-personal") {
						t.Fatal("refresh not bound to rejected generation")
					}
					if rotated {
						currentToken = "new-personal"
					}
				}
				return currentToken, nil
			})
			err := NewClient(&Config{BaseURL: ts.URL}).DoRequest(ctx, http.MethodGet, "/target", nil, nil)
			if rotated && err != nil {
				t.Fatal(err)
			}
			if !rotated && !errors.Is(err, ErrAuth) {
				t.Fatalf("want original auth error, got %v", err)
			}
			want := 1
			if rotated {
				want = 2
			}
			if calls != want || hints != 1 || tenantCalls != 0 {
				t.Fatalf("calls=%d hints=%d tenant=%d", calls, hints, tenantCalls)
			}
			data, _ := json.Marshal(events)
			if strings.Contains(string(data), "request-opaque") ||
				strings.Contains(string(data), "personal") {
				// Operation names can contain personal; this operation is deliberately /target.
				t.Fatal("diagnostics exposed request ID or credential")
			}
			if len(events) != want || events[0].RequestIDHash == "" {
				t.Fatal("missing safe per-request receipt")
			}
		})
	}
}

func TestScopeAndExportDenialsNeverRefreshOrChangeIdentity(t *testing.T) {
	for _, code := range []int{99991672, 99991676, 99991679, 1069902} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			calls := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(http.StatusForbidden)
				_, _ = fmt.Fprintf(
					w,
					`{"code":%d,"msg":"need [docs:document:export, drive:export:readonly]; sensitive body"}`,
					code,
				)
			}))
			defer ts.Close()
			var event APIObservation
			ctx := WithAPIObserver(context.Background(), func(e APIObservation) { event = e })
			ctx = WithUserIdentity(ctx, func(ctx context.Context) (string, error) {
				if UserTokenRefreshFingerprint(ctx) != "" {
					t.Fatal("permission denial triggered refresh")
				}
				return "personal", nil
			})
			err := NewClient(&Config{BaseURL: ts.URL}).DoRequest(ctx, http.MethodGet, "/target", nil, nil)
			if err == nil || calls != 1 || len(event.RequiredScopes) != 2 {
				t.Fatalf("calls=%d receipt=%+v error=%v", calls, event, err)
			}
			if code == 99991672 && !errors.Is(err, ErrAppScope) {
				t.Fatal("application scope confused with export refusal")
			}
			if strings.Contains(err.Error(), "sensitive") {
				t.Fatal("provider message exposed")
			}
		})
	}
}
