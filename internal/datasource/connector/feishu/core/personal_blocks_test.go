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

func TestPersonalBlocksFallbackRequiresSourcePermissionAndCompleteBody(t *testing.T) {
	for _, scenario := range []string{
		"allowed", "export_denied", "policy_scope_missing", "policy_field_missing", "permission_revoked",
		"changed_revision", "image", "unknown_inline", "incomplete_page", "cycle", "transient_export",
	} {
		t.Run(scenario, func(t *testing.T) {
			var calls []string
			permissionCalls, blockCalls := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer personal-user" {
					t.Error("unexpected identity or application-token fallback")
					w.WriteHeader(401)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				var value any
				switch r.URL.Path {
				case "/open-apis/drive/v1/export_tasks":
					calls = append(calls, "export")
					if scenario == "transient_export" {
						w.WriteHeader(503)
						value = map[string]any{"code": 1069999}
					} else {
						w.WriteHeader(403)
						value = map[string]any{"code": 1069902}
					}
				case "/open-apis/drive/v1/permissions/real/members/auth":
					calls = append(calls, "permission")
					permissionCalls++
					allowed := !(scenario == "export_denied" && r.URL.Query().Get("action") == "export") &&
						!(scenario == "permission_revoked" && permissionCalls > 2)
					value = map[string]any{"code": 0, "data": map[string]any{"auth_result": allowed}}
					if scenario == "policy_scope_missing" {
						w.WriteHeader(400)
						value = map[string]any{"code": 99991679}
					} else if scenario == "policy_field_missing" {
						value = map[string]any{"code": 0, "data": map[string]any{}}
					}
				case "/open-apis/docx/v1/documents/real/blocks":
					calls = append(calls, "blocks")
					blockCalls++
					children := []string{"paragraph"}
					if scenario == "cycle" {
						children = []string{"real"}
					}
					paragraph := map[string]any{
						"block_id": "paragraph", "block_type": 2,
						"text": map[string]any{"elements": []any{map[string]any{
							"text_run": map[string]any{"content": "Readable authorized text"},
						}}},
					}
					if scenario == "image" {
						paragraph = map[string]any{
							"block_id": "paragraph", "block_type": 27,
							"image": map[string]any{"token": "image-token"},
						}
					} else if scenario == "unknown_inline" {
						paragraph["text"] = map[string]any{"elements": []any{map[string]any{
							"equation": map[string]any{"content": "x^2"},
						}}}
					}
					value = map[string]any{"code": 0, "data": map[string]any{
						"items": []any{
							map[string]any{"block_id": "real", "block_type": 1, "children": children},
							paragraph,
						},
						"has_more": scenario == "incomplete_page", "page_token": "",
					}}
				case "/open-apis/drive/v1/metas/batch_query":
					modified := "1000"
					if scenario == "changed_revision" {
						modified = "1001"
					}
					value = map[string]any{"code": 0, "data": map[string]any{"metas": []any{map[string]any{
						"doc_token": "real", "doc_type": "docx", "title": "test", "latest_modify_time": modified,
					}}}}
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
					w.WriteHeader(404)
				}
				_ = json.NewEncoder(w).Encode(value)
			}))
			defer server.Close()
			client := NewClient(&Config{AppID: "blocks-" + scenario, BaseURL: server.URL})
			ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) {
				return "personal-user", nil
			})
			body, name, err := client.DownloadPersonalResource(ctx, PersonalResource{
				Type: "docx", Token: "real", Title: "test", Modified: "1000",
			})
			if len(calls) == 0 || calls[0] != "export" {
				t.Fatal("export must remain the first route")
			}
			if scenario == "allowed" {
				if err != nil || name != "test.md" || !strings.Contains(string(body), "Readable authorized text") ||
					permissionCalls != 4 || blockCalls != 1 {
					t.Fatalf(
						"allowed fallback failed: name=%s err=%v permissions=%d blocks=%d",
						name,
						err,
						permissionCalls,
						blockCalls,
					)
				}
				return
			}
			if err == nil || len(body) != 0 || name != "" {
				t.Fatal("unverified/partial content must never be returned as an import")
			}
			switch scenario {
			case "export_denied", "policy_scope_missing", "policy_field_missing", "transient_export":
				if blockCalls != 0 {
					t.Fatal("read route bypassed source permission or retry gate")
				}
			case "image", "unknown_inline", "incomplete_page", "cycle":
				if !errors.Is(err, ErrPersonalBlocksIncomplete) {
					t.Fatalf("wrong partial-body classification: %v", err)
				}
			}
		})
	}
}
