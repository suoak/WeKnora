package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestP4AttachmentDownloadIsBoundToRecordAndContent(t *testing.T) {
	for _, mode := range []string{"success", "revoked", "short", "identity_changed"} {
		t.Run(mode, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var data any
				switch {
				case strings.HasSuffix(r.URL.Path, "/tables"):
					data = map[string]any{"items": []any{map[string]any{"table_id": "t", "name": "T"}}}
				case strings.HasSuffix(r.URL.Path, "/fields"):
					data = map[string]any{
						"items": []any{
							map[string]any{"field_id": "f", "field_name": "Attachment", "type": 17},
						},
					}
				case strings.HasSuffix(r.URL.Path, "/records"):
					data = map[string]any{
						"items": []any{
							map[string]any{
								"record_id": "r",
								"fields": map[string]any{
									"Attachment": []any{
										map[string]any{
											"file_token": "token",
											"name":       "a.txt",
											"type":       "text/plain",
											"size":       5,
											"tmp_url":    "https://expired.invalid",
										},
									},
								},
							},
						},
						"total": 1,
					}
				case strings.HasSuffix(r.URL.Path, "/medias/token/download"):
					var extra struct {
						Permission struct {
							TableID     string                         `json:"tableId"`
							Attachments map[string]map[string][]string `json:"attachments"`
						} `json:"bitablePerm"`
					}
					if json.Unmarshal([]byte(r.URL.Query().Get("extra")), &extra) != nil ||
						extra.Permission.TableID != "t" ||
						len(extra.Permission.Attachments["f"]["r"]) != 1 {
						t.Error("download is not record bound")
					}
					if mode == "revoked" {
						w.WriteHeader(403)
						_, _ = fmt.Fprint(w, `{"code":1770032}`)
						return
					}
					if mode == "short" {
						_, _ = fmt.Fprint(w, "bad")
						return
					}
					_, _ = fmt.Fprint(w, "hello")
					return
				default:
					t.Errorf("unexpected %s", r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
			}))
			defer s.Close()
			ctx := WithApplicationIdentity(
				context.Background(),
				func(context.Context) (string, error) { return "app", nil },
			)
			ctx = WithApplicationAttachmentProcessor(
				ctx,
				func(_ context.Context, a ApplicationAttachment) (ApplicationAttachment, error) {
					a.Text = "hello"
					a.Parser = "fixture-parser"
					if mode == "identity_changed" {
						a.RecordID = "other"
					}
					return a, nil
				},
			)
			c := NewClient(&Config{AppID: "p4asset" + mode, BaseURL: s.URL})
			snap, err := c.CollectApplicationTableSnapshot(
				ctx,
				PersonalResource{Type: "bitable", Token: "base"},
			)
			if mode != "success" {
				if err == nil {
					t.Fatal("unsafe attachment accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(snap.Attachments) != 1 || string(snap.Attachments[0].Data) != "hello" {
				t.Fatal("original attachment lost")
			}
			text, err := snap.IndexText()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(text, "hello") || strings.Contains(text, "expired.invalid") {
				t.Fatal("incomplete or expiring attachment reference indexed")
			}
		})
	}
}
