package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/logger"
	"golang.org/x/time/rate"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func mustTestClient(t *testing.T, token, baseURL string) *notionClient {
	t.Helper()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
	secutils.ResetSSRFWhitelistForTest()

	client, err := newClient(token, baseURL)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return client
}

// fakeNotion builds an httptest.Server that emulates relevant Notion API endpoints.
func fakeNotion() (*httptest.Server, *Config) {
	mux := http.NewServeMux()

	// GET /v1/users/me — auth check
	mux.HandleFunc("/v1/users/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":  401,
				"code":    "unauthorized",
				"message": "API token is invalid.",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "user",
			"id":     "bot-user-id",
			"type":   "bot",
			"name":   "Test Integration",
		})
	})

	// POST /v1/search — return pages and databases
	mux.HandleFunc("/v1/search", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"results": []interface{}{
				map[string]interface{}{
					"id":               "page-1",
					"object":           "page",
					"url":              "https://notion.so/Page-1",
					"last_edited_time": "2026-01-15T10:00:00.000Z",
					"in_trash":         false,
					"parent":           map[string]interface{}{"type": "workspace", "workspace": true},
					"properties": map[string]interface{}{
						"title": map[string]interface{}{
							"type": "title",
							"title": []interface{}{
								map[string]interface{}{"plain_text": "Test Page"},
							},
						},
					},
				},
				map[string]interface{}{
					"id":               "db-1",
					"object":           "data_source",
					"url":              "https://notion.so/DB-1",
					"last_edited_time": "2026-01-16T10:00:00.000Z",
					"in_trash":         false,
					"parent":           map[string]interface{}{"type": "workspace", "workspace": true},
					"title": []interface{}{
						map[string]interface{}{"plain_text": "Test Database"},
					},
				},
			},
			"has_more":    false,
			"next_cursor": nil,
		})
	})

	// GET /v1/blocks/{id}/children — return blocks
	mux.HandleFunc("/v1/blocks/page-1/children", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"results": []interface{}{
				map[string]interface{}{
					"id":           "blk-1",
					"type":         "paragraph",
					"has_children": false,
					"paragraph": map[string]interface{}{
						"rich_text": []interface{}{
							map[string]interface{}{
								"type":       "text",
								"plain_text": "Hello world",
								"text":       map[string]interface{}{"content": "Hello world"},
								"annotations": map[string]interface{}{
									"bold": false, "italic": false, "strikethrough": false,
									"underline": false, "code": false, "color": "default",
								},
							},
						},
					},
				},
				map[string]interface{}{
					"id":           "blk-2",
					"type":         "child_page",
					"has_children": true,
					"child_page":   map[string]interface{}{"title": "Sub Page"},
				},
			},
			"has_more":    false,
			"next_cursor": nil,
		})
	})

	// GET /v1/pages/{id}
	mux.HandleFunc("/v1/pages/page-1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":               "page-1",
			"object":           "page",
			"url":              "https://notion.so/Page-1",
			"last_edited_time": "2026-01-15T10:00:00.000Z",
			"in_trash":         false,
			"parent":           map[string]interface{}{"type": "workspace", "workspace": true},
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type": "title",
					"title": []interface{}{
						map[string]interface{}{"plain_text": "Test Page"},
					},
				},
			},
		})
	})

	// GET /v1/databases/{id} — returns container with data_sources array (2025-09-03+)
	mux.HandleFunc("/v1/databases/db-1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":               "db-1",
			"object":           "database",
			"url":              "https://notion.so/DB-1",
			"last_edited_time": "2026-01-16T10:00:00.000Z",
			"in_trash":         false,
			"parent":           map[string]interface{}{"type": "workspace", "workspace": true},
			"title":            []interface{}{map[string]interface{}{"plain_text": "Test Database"}},
			"data_sources": []interface{}{
				map[string]interface{}{"id": "ds-1", "name": "Default"},
			},
		})
	})

	// GET /v1/data_sources/{id} — returns schema/properties
	mux.HandleFunc("/v1/data_sources/ds-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":     "ds-1",
				"object": "data_source",
				"properties": map[string]interface{}{
					"Name":   map[string]interface{}{"type": "title", "title": map[string]interface{}{}},
					"Status": map[string]interface{}{"type": "select", "select": map[string]interface{}{}},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	// POST /v1/data_sources/{id}/query — return database records (2025-09-03+)
	mux.HandleFunc("/v1/data_sources/ds-1/query", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"results": []interface{}{
				map[string]interface{}{
					"id":               "record-1",
					"object":           "page",
					"url":              "https://notion.so/Record-1",
					"last_edited_time": "2026-01-17T10:00:00.000Z",
					"in_trash":         false,
					"parent":           map[string]interface{}{"type": "data_source_id", "data_source_id": "ds-1"},
					"properties": map[string]interface{}{
						"Name": map[string]interface{}{
							"type": "title",
							"title": []interface{}{
								map[string]interface{}{"plain_text": "Record One"},
							},
						},
						"Status": map[string]interface{}{
							"type":   "select",
							"select": map[string]interface{}{"name": "Done"},
						},
					},
				},
			},
			"has_more":    false,
			"next_cursor": nil,
		})
	})

	// GET /v1/pages/record-1 — direct fetch of a database row (single-select case).
	// fetchPage must recognize both database_id (older responses) and
	// data_source_id (2025-09-03+) as a record and route to buildRecordItem.
	mux.HandleFunc("/v1/pages/record-1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":               "record-1",
			"object":           "page",
			"url":              "https://notion.so/Record-1",
			"last_edited_time": "2026-01-17T10:00:00.000Z",
			"in_trash":         false,
			"parent":           map[string]interface{}{"type": "data_source_id", "data_source_id": "ds-1"},
			"properties": map[string]interface{}{
				"Name": map[string]interface{}{
					"type": "title",
					"title": []interface{}{
						map[string]interface{}{"plain_text": "Record One"},
					},
				},
				"Status": map[string]interface{}{
					"type":   "select",
					"select": map[string]interface{}{"name": "Done"},
				},
			},
		})
	})

	// GET /v1/blocks/record-1/children — empty blocks for database record
	mux.HandleFunc("/v1/blocks/record-1/children", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"object":      "list",
			"results":     []interface{}{},
			"has_more":    false,
			"next_cursor": nil,
		})
	})

	ts := httptest.NewServer(mux)
	cfg := &Config{APIKey: "test-token"}
	return ts, cfg
}

func TestClientPing(t *testing.T) {
	ts, cfg := fakeNotion()
	defer ts.Close()

	client := mustTestClient(t, cfg.APIKey, ts.URL)
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestClientPing_InvalidToken(t *testing.T) {
	ts, _ := fakeNotion()
	defer ts.Close()

	client := mustTestClient(t, "wrong-token", ts.URL)
	err := client.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestClientSearchPages(t *testing.T) {
	ts, cfg := fakeNotion()
	defer ts.Close()

	client := mustTestClient(t, cfg.APIKey, ts.URL)
	pages, err := client.SearchPages(context.Background())
	if err != nil {
		t.Fatalf("SearchPages() error: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages))
	}
	// First result is a page
	if pages[0].Object != "page" || pages[0].ID != "page-1" {
		t.Errorf("page[0] = %+v", pages[0])
	}
	// Second result is a data_source (databases are returned as object="data_source"
	// in API 2025-09-03+ which is what the mock simulates)
	if pages[1].Object != "data_source" || pages[1].ID != "db-1" {
		t.Errorf("page[1] = %+v", pages[1])
	}
}

func TestClientGetBlockChildrenAll(t *testing.T) {
	ts, cfg := fakeNotion()
	defer ts.Close()

	client := mustTestClient(t, cfg.APIKey, ts.URL)
	blocks, err := client.GetBlockChildrenAll(context.Background(), "page-1")
	if err != nil {
		t.Fatalf("GetBlockChildrenAll() error: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Type != "paragraph" {
		t.Errorf("block[0].Type = %q", blocks[0].Type)
	}
	if blocks[1].Type != "child_page" {
		t.Errorf("block[1].Type = %q", blocks[1].Type)
	}
	// child_page should NOT have its children recursed
	if blocks[1].Children != nil {
		t.Error("child_page children should not be recursed by client")
	}
}

func TestClientGetPage(t *testing.T) {
	ts, cfg := fakeNotion()
	defer ts.Close()

	client := mustTestClient(t, cfg.APIKey, ts.URL)
	page, err := client.GetPage(context.Background(), "page-1")
	if err != nil {
		t.Fatalf("GetPage() error: %v", err)
	}
	if page.ID != "page-1" {
		t.Errorf("ID = %q", page.ID)
	}
	if page.Title != "Test Page" {
		t.Errorf("Title = %q, want %q", page.Title, "Test Page")
	}
}

func TestClientQueryDatabaseAll(t *testing.T) {
	ts, cfg := fakeNotion()
	defer ts.Close()

	client := mustTestClient(t, cfg.APIKey, ts.URL)
	records, err := client.QueryDatabaseAll(context.Background(), "db-1")
	if err != nil {
		t.Fatalf("QueryDatabaseAll() error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].ID != "record-1" {
		t.Errorf("record ID = %q", records[0].ID)
	}
}

// Ensure time import is used (referenced by fakeNotion indirectly via time.Time in types).
var _ = time.Now

func TestDownloadFile_RejectsLoopbackURL(t *testing.T) {
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	client, err := newClient("test-token", "https://api.notion.com")
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	_, err = client.DownloadFile(context.Background(), "http://127.0.0.1/secret")
	if err == nil {
		t.Fatal("expected loopback attachment URL to be rejected")
	}
}

type a4BrokenJSONReader struct{}

func (a4BrokenJSONReader) Read(p []byte) (int, error) {
	copy(p, "partial")
	return 7, io.ErrUnexpectedEOF
}
func TestA4JSONCapReader(t *testing.T) {
	for _, n := range []int64{maxJSONResponseBytes - 1, maxJSONResponseBytes, maxJSONResponseBytes + 1} {
		data, err := readCappedJSON(bytes.NewReader(bytes.Repeat([]byte("x"), int(n))))
		if n > maxJSONResponseBytes {
			if data != nil || !errors.Is(err, ErrResponseTooLarge) {
				t.Fatal("overflow returned partial JSON")
			}
		} else if err != nil || int64(len(data)) != n {
			t.Fatal("legal boundary rejected")
		}
	}
	if data, err := readCappedJSON(a4BrokenJSONReader{}); data != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("interruption returned partial JSON")
	}
}
func TestA4JSONCapHTTPAndSafeProviderErrors(t *testing.T) {
	for _, mode := range []string{"known", "chunked", "auth", "auth-cap"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if mode == "auth" {
					w.WriteHeader(403)
					_, _ = io.WriteString(w, `{"code":110030,"msg":"TOKEN-CANARY SECRET-CANARY SQL-CANARY Authorization: Bearer SYNTHETIC","message":"TOKEN-CANARY"}`)
					return
				}
				if mode == "auth-cap" {
					w.WriteHeader(403)
				}
				if mode == "known" {
					w.Header().Set("Content-Length", fmt.Sprint(maxJSONResponseBytes+1))
				} else if mode != "auth-cap" {
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				_, _ = io.Copy(w, strings.NewReader(strings.Repeat("x", int(maxJSONResponseBytes+1))))
			}))
			defer srv.Close()
			var logs bytes.Buffer
			logger.SetOutput(&logs)
			defer logger.SetOutput(os.Stderr)
			c := &notionClient{baseURL: srv.URL, httpClient: srv.Client(), limiter: rate.NewLimiter(rate.Inf, 1)}
			_, err := c.doRequest(context.Background(), http.MethodGet, "/synthetic", nil)
			if err == nil || calls != 1 {
				t.Fatalf("failure/retry: err=%v calls=%d", err, calls)
			}
			if (mode == "auth" || mode == "auth-cap") && !errors.Is(err, datasource.ErrInvalidCredentials) {
				t.Fatal("authentication classification lost")
			}
			if mode != "auth" && !errors.Is(err, ErrResponseTooLarge) {
				t.Fatalf("cap classification: %v", err)
			}
			for _, marker := range []string{"TOKEN-CANARY", "SECRET-CANARY", "SQL-CANARY", "Authorization:", "SYNTHETIC"} {
				if strings.Contains(err.Error(), marker) || strings.Contains(logs.String(), marker) {
					t.Fatal("canary leaked")
				}
			}
		})
	}
}

// This is a safety assertion, not a characterization expecting unsafe success.
// Keep the FAIL if an oversized page body is acknowledged and checkpointed.
func TestA4NotionOversizedPageMustNotAdvanceCursor(t *testing.T) {
	allowNotionTestServer(t)
	blocksRequests := 0
	oversizedJSON := `{"results":[{"id":"synthetic-block","type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"` + strings.Repeat("x", int(maxJSONResponseBytes)) + `"}}]}}],"has_more":false}`
	if !json.Valid([]byte(oversizedJSON)) {
		t.Fatal("oversized fixture must be valid JSON")
	}
	oldTime := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Hour)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"id": "synthetic-page", "object": "page", "last_edited_time": newTime, "parent": map[string]any{"type": "workspace", "workspace": true}}}, "has_more": false})
		case "/v1/blocks/synthetic-page/children":
			blocksRequests++
			w.Header().Set("Content-Length", fmt.Sprint(len(oversizedJSON)))
			_, _ = io.WriteString(w, oversizedJSON)
		default:
			t.Errorf("unexpected isolated request path")
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	config := makeNotionConfig(&Config{APIKey: "synthetic-key"}, srv.URL, []string{"synthetic-page"})
	items, next, err := NewConnector().FetchIncremental(context.Background(), config, buildCursor(map[string]time.Time{"synthetic-page": oldTime}))
	advanced := false
	if next != nil {
		var decoded notionCursor
		b, _ := json.Marshal(next.ConnectorCursor)
		_ = json.Unmarshal(b, &decoded)
		advanced = decoded.PageEditTimes["synthetic-page"].Equal(newTime)
	}
	t.Logf("isolated HTTP blocksRequests=%d; emitted=%d; errNil=%t; nextCursor=%t; failedPageAdvanced=%t", blocksRequests, len(items), err == nil, next != nil, advanced)
	if blocksRequests != 1 {
		t.Fatal("oversized request was omitted or automatically retried")
	}
	if err == nil && len(items) == 0 {
		t.Error("oversized content silently acknowledged as complete sync")
	}
	if advanced {
		t.Error("failed page edit time advanced despite response size failure")
	}
}

func TestA4NotionIncompleteFetchRetainsCursorAndRetries(t *testing.T) {
	for _, kind := range []string{"page", "database", "database-pagination", "record", "child-page"} {
		t.Run(kind, func(t *testing.T) {
			allowNotionTestServer(t)
			old := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
			now := old.Add(time.Hour)
			var broken atomic.Bool
			broken.Store(true)
			page := func(id string) map[string]any {
				return map[string]any{"id": id, "object": "page", "last_edited_time": now, "parent": map[string]any{"type": "workspace", "workspace": true}}
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body any
				switch r.URL.Path {
				case "/v1/search":
					root := page("bad")
					if kind == "database" || kind == "database-pagination" || kind == "record" {
						root["object"] = "data_source"
					}
					body = map[string]any{"results": []any{root, page("good")}, "has_more": false}
				case "/v1/data_sources/bad":
					body = map[string]any{"id": "bad", "object": "data_source"}
				case "/v1/data_sources/bad/query":
					var query map[string]any
					_ = json.NewDecoder(r.Body).Decode(&query)
					if (kind == "database" || (kind == "database-pagination" && query["start_cursor"] != nil)) && broken.Load() {
						w.WriteHeader(403)
						_, _ = io.WriteString(w, `{"message":"SECRET-CANARY"}`)
						return
					}
					body = map[string]any{"results": []any{page("record")}, "has_more": false}
					if kind == "database-pagination" && broken.Load() {
						body = map[string]any{"results": []any{page("record")}, "has_more": true, "next_cursor": "page-2"}
					}
				case "/v1/blocks/bad/children":
					if kind == "page" && broken.Load() {
						w.WriteHeader(403)
						return
					}
					if kind == "child-page" {
						body = map[string]any{"results": []any{map[string]any{"id": "child", "type": "child_page"}}, "has_more": false}
					} else {
						body = map[string]any{"results": []any{}, "has_more": false}
					}
				case "/v1/pages/child":
					if broken.Load() {
						w.WriteHeader(403)
						return
					}
					body = page("child")
				case "/v1/blocks/record/children":
					if kind == "record" && broken.Load() {
						w.WriteHeader(403)
						return
					}
					body = map[string]any{"results": []any{}, "has_more": false}
				case "/v1/blocks/good/children", "/v1/blocks/child/children":
					body = map[string]any{"results": []any{}, "has_more": false}
				default:
					w.WriteHeader(404)
					return
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer srv.Close()
			cfg := makeNotionConfig(&Config{APIKey: "synthetic-key"}, srv.URL, []string{"bad", "good"})
			previous := map[string]time.Time{"bad": old, "good": old, "historical-record": old}
			items, next, err := NewConnector().FetchIncremental(context.Background(), cfg, buildCursor(previous))
			var partial *datasource.PartialFetchError
			if !errors.As(err, &partial) {
				t.Fatal("incomplete fetch must report partial failure")
			}
			if strings.Contains(err.Error(), "SECRET-CANARY") {
				t.Fatal("provider body leaked")
			}
			good := false
			for _, item := range items {
				if item.IsDeleted || item.ExternalID == "bad" {
					t.Fatal("failure generated deletion or successful parent")
				}
				good = good || item.ExternalID == "good"
			}
			if !good {
				t.Fatal("successful sibling lost")
			}
			var retained notionCursor
			data, _ := json.Marshal(next.ConnectorCursor)
			_ = json.Unmarshal(data, &retained)
			for id, value := range previous {
				if !retained.PageEditTimes[id].Equal(value) {
					t.Fatal("partial checkpoint advanced or dropped")
				}
			}
			broken.Store(false)
			for attempt := 0; attempt < 2; attempt++ {
				items, next, err = NewConnector().FetchIncremental(context.Background(), cfg, next)
				if err != nil {
					t.Fatalf("explicit retry failed: %v", err)
				}
				seen := map[string]bool{}
				for _, item := range items {
					if seen[item.ExternalID] {
						t.Fatal("duplicate external ID")
					}
					seen[item.ExternalID] = true
				}
			}
			t.Log("partial failure preserved successful sibling and old cursor; explicit retries completed")
		})
	}
}

// Keep this safety assertion failing until the separately frozen Client path is approved.
func TestA4NotionNestedBlockFailureMustNotAcknowledgePage(t *testing.T) {
	allowNotionTestServer(t)
	old := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"id": "page", "object": "page", "last_edited_time": old.Add(time.Hour), "parent": map[string]any{"type": "workspace", "workspace": true}}}, "has_more": false})
		case "/v1/blocks/page/children":
			_, _ = io.WriteString(w, `{"results":[{"id":"nested","type":"paragraph","has_children":true,"paragraph":{"rich_text":[{"type":"text","text":{"content":"parent"}}]}}],"has_more":false}`)
		default:
			w.WriteHeader(403)
		}
	}))
	defer srv.Close()
	cfg := makeNotionConfig(&Config{APIKey: "synthetic-key"}, srv.URL, []string{"page"})
	items, next, err := NewConnector().FetchIncremental(context.Background(), cfg, buildCursor(map[string]time.Time{"page": old}))
	t.Logf("nested HTTP403: items=%d error=%t cursor=%t", len(items), err != nil, next != nil)
	if err == nil {
		t.Error("nested failure silently acknowledged as complete page")
	}
	if len(items) > 0 {
		t.Error("partial parent content emitted as complete")
	}
}

func TestA4NotionCanceledPageRetainsCheckpoint(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			allowNotionTestServer(t)
			old := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
			entered := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/search" {
					_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"id": "page", "object": "page", "last_edited_time": old.Add(time.Hour), "parent": map[string]any{"type": "workspace", "workspace": true}}}, "has_more": false})
					return
				}
				close(entered)
				<-r.Context().Done()
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if mode == "cancel" {
				go func() {
					select {
					case <-entered:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			cfg := makeNotionConfig(&Config{APIKey: "synthetic-key"}, srv.URL, []string{"page"})
			items, next, err := NewConnector().FetchIncremental(ctx, cfg, buildCursor(map[string]time.Time{"page": old}))
			if err == nil || len(items) != 0 || next == nil || ctx.Err() == nil {
				t.Fatal("canceled read acknowledged or lost checkpoint")
			}
			var decoded notionCursor
			data, _ := json.Marshal(next.ConnectorCursor)
			_ = json.Unmarshal(data, &decoded)
			if !decoded.PageEditTimes["page"].Equal(old) {
				t.Fatal("canceled page advanced")
			}
			t.Logf("real scheduling mode=%s context=%v emitted=0 checkpoint=old", mode, ctx.Err())
		})
	}
}

func TestA4NotionDatabaseFallbackOnlyOnNotFound(t *testing.T) {
	for _, operation := range []string{"query", "metadata"} {
		t.Run(operation, func(t *testing.T) {
			for _, mode := range []string{"success", "multipage", "404", "401", "403", "429", "500", "cap", "cancel", "deadline", "interrupted", "pagination-interrupted"} {
				if operation == "metadata" && (mode == "pagination-interrupted" || mode == "multipage") {
					continue
				}
				t.Run(mode, func(t *testing.T) {
					var primary, fallback atomic.Int64
					entered := make(chan struct{}, 1)
					release := make(chan struct{})
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					if mode == "deadline" {
						cancel()
						ctx, cancel = context.WithTimeout(context.Background(), time.Second)
					}
					defer cancel()
					if mode == "cancel" {
						go func() {
							select {
							case <-entered:
								cancel()
							case <-ctx.Done():
							}
						}()
					}
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						if r.URL.Path == "/v1/databases/root" {
							fallback.Add(1)
							_, _ = io.WriteString(w, `{"id":"root","object":"database","data_sources":[{"id":"canonical"}]}`)
							return
						}
						if r.URL.Path == "/v1/data_sources/canonical/query" {
							_, _ = io.WriteString(w, `{"results":[{"id":"record","object":"page"}],"has_more":false}`)
							return
						}
						primary.Add(1)
						select {
						case entered <- struct{}{}:
						default:
						}
						switch mode {
						case "multipage":
							var query map[string]any
							_ = json.NewDecoder(r.Body).Decode(&query)
							if query["start_cursor"] == nil {
								_, _ = io.WriteString(w, `{"results":[{"id":"first","object":"page"}],"has_more":true,"next_cursor":"second"}`)
							} else {
								_, _ = io.WriteString(w, `{"results":[{"id":"second","object":"page"}],"has_more":false}`)
							}
							return
						case "404", "401", "403", "429", "500":
							status, _ := strconv.Atoi(mode)
							w.Header().Set("Retry-After", "0")
							w.WriteHeader(status)
							_, _ = io.WriteString(w, `{"message":"TOKEN-CANARY SECRET-CANARY SQL-CANARY Authorization: Bearer SYNTHETIC"}`)
							return
						case "cap":
							w.Header().Set("Content-Length", fmt.Sprint(maxJSONResponseBytes+1))
							_, _ = io.Copy(w, strings.NewReader(strings.Repeat("x", int(maxJSONResponseBytes+1))))
							return
						case "cancel", "deadline":
							select {
							case <-r.Context().Done():
							case <-release:
							}
							return
						case "pagination-interrupted":
							var query map[string]any
							_ = json.NewDecoder(r.Body).Decode(&query)
							if query["start_cursor"] == nil {
								_, _ = io.WriteString(w, `{"results":[{"id":"record","object":"page"}],"has_more":true,"next_cursor":"second"}`)
								return
							}
							fallthrough
						case "interrupted":
							// Send an incomplete body over real HTTP; the Client must not
							// interpret this as a type mismatch or return partial records.
							w.Header().Set("Content-Length", "200")
							_, _ = io.WriteString(w, `{"results":`)
							return
						}
						if operation == "query" {
							_, _ = io.WriteString(w, `{"results":[{"id":"record","object":"page"}],"has_more":false}`)
						} else {
							_, _ = io.WriteString(w, `{"id":"root","object":"data_source"}`)
						}
					}))
					defer srv.Close()
					defer close(release)
					client := &notionClient{baseURL: srv.URL, httpClient: srv.Client(), limiter: rate.NewLimiter(rate.Inf, 1)}
					var logs bytes.Buffer
					logger.SetOutput(&logs)
					defer logger.SetOutput(os.Stderr)
					var err error
					if operation == "query" {
						var records []notionPage
						records, err = client.QueryDatabaseAll(ctx, "root")
						if mode == "multipage" {
							if err != nil || len(records) != 2 || primary.Load() != 2 || records[0].ID != "first" || records[1].ID != "second" {
								t.Fatal("legal multipage query lost records or pagination")
							}
						} else if mode == "success" || mode == "404" {
							if len(records) != 1 {
								t.Fatal("normal query lost records")
							}
						} else if records != nil {
							t.Fatal("failed query returned partial records")
						}
					} else {
						var info *databaseInfo
						info, err = getDatabaseOrDataSourceInfo(ctx, client, "root")
						if mode == "success" || mode == "404" {
							if info == nil {
								t.Fatal("normal metadata lost")
							}
						} else if info != nil {
							t.Fatal("failed metadata became valid")
						}
					}
					if mode == "success" || mode == "multipage" || mode == "404" {
						if err != nil {
							t.Fatalf("legal query/fallback failed: %v", err)
						}
						want := int64(0)
						if mode == "404" {
							want = 1
						}
						if fallback.Load() != want {
							t.Fatal("incorrect legal fallback count")
						}
					} else {
						if err == nil || fallback.Load() != 0 {
							t.Fatal("non-404 failure masked by database fallback")
						}
						if errors.Is(err, datasource.ErrResourceNotFound) {
							t.Fatal("non-404 classified as absent")
						}
						if (mode == "401" || mode == "403") && !errors.Is(err, datasource.ErrInvalidCredentials) {
							t.Fatal("credentials classification lost")
						}
						if mode == "cap" && !errors.Is(err, ErrResponseTooLarge) {
							t.Fatal("cap classification lost")
						}
						if mode == "cancel" && !errors.Is(err, context.Canceled) {
							t.Fatal("cancel classification lost")
						}
						if mode == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
							t.Fatal("deadline classification lost")
						}
						for _, marker := range []string{"TOKEN-CANARY", "SECRET-CANARY", "SQL-CANARY", "Authorization:", "SYNTHETIC"} {
							if strings.Contains(err.Error(), marker) || strings.Contains(logs.String(), marker) {
								t.Fatal("provider credential leaked")
							}
						}
					}
					t.Logf("real HTTP operation=%s status=%s primary=%d fallback=%d failed=%t", operation, mode, primary.Load(), fallback.Load(), err != nil)
				})
			}
		})
	}
}

func TestA4NotionNestedIntegrityFailureAndExplicitRetry(t *testing.T) {
	for _, mode := range []string{"403", "cap", "deep", "404"} {
		t.Run(mode, func(t *testing.T) {
			allowNotionTestServer(t)
			var broken atomic.Bool
			broken.Store(true)
			var downloads atomic.Int64
			old := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
			page := func(id string) map[string]any {
				return map[string]any{"id": id, "object": "page", "last_edited_time": old.Add(time.Hour), "parent": map[string]any{"type": "workspace", "workspace": true}}
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/search" {
					_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{page("bad"), page("good")}, "has_more": false})
					return
				}
				if r.URL.Path == "/v1/blocks/bad/children" || (mode == "deep" && r.URL.Path == "/v1/blocks/nested/children") {
					id := "nested"
					if r.URL.Path == "/v1/blocks/nested/children" {
						id = "deep"
					}
					paragraph := map[string]any{"rich_text": []any{map[string]any{"type": "text", "text": map[string]any{"content": "parent"}}}}
					blocks := []any{map[string]any{"id": id, "type": "paragraph", "has_children": true, "paragraph": paragraph}}
					if r.URL.Path == "/v1/blocks/bad/children" && mode != "404" && broken.Load() {
						blocks = append(blocks, map[string]any{"id": "attachment-block", "type": "file", "file": map[string]any{"type": "external", "name": "synthetic.pdf", "external": map[string]any{"url": "http://" + r.Host + "/attachment"}}})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"results": blocks, "has_more": false})
					return
				}
				if r.URL.Path == "/v1/blocks/nested/children" || r.URL.Path == "/v1/blocks/deep/children" {
					if broken.Load() {
						switch mode {
						case "cap":
							w.Header().Set("Content-Length", fmt.Sprint(maxJSONResponseBytes+1))
							_, _ = io.Copy(w, strings.NewReader(strings.Repeat("x", int(maxJSONResponseBytes+1))))
						case "404":
							w.WriteHeader(404)
						default:
							w.WriteHeader(403)
							_, _ = io.WriteString(w, `{"message":"SECRET-CANARY"}`)
						}
						return
					}
				}
				if r.URL.Path == "/attachment" {
					downloads.Add(1)
					t.Error("failed parent attachment downloaded")
					return
				}
				_, _ = io.WriteString(w, `{"results":[],"has_more":false}`)
			}))
			defer srv.Close()
			cfg := makeNotionConfig(&Config{APIKey: "synthetic-key"}, srv.URL, []string{"bad", "good"})
			prior := buildCursor(map[string]time.Time{"bad": old, "good": old})
			items, next, err := NewConnector().FetchIncremental(context.Background(), cfg, prior)
			if mode == "404" {
				if err != nil || len(items) != 2 {
					t.Fatal("confirmed absent nested child tolerance changed")
				}
			} else {
				var partial *datasource.PartialFetchError
				if !errors.As(err, &partial) {
					t.Fatal("nested error not propagated")
				}
				if len(items) != 1 || items[0].ExternalID != "good" || items[0].IsDeleted {
					t.Fatal("failed parent emitted or sibling lost")
				}
				var state notionCursor
				b, _ := json.Marshal(next.ConnectorCursor)
				_ = json.Unmarshal(b, &state)
				if !state.PageEditTimes["bad"].Equal(old) {
					t.Fatal("failed parent checkpoint advanced")
				}
			}
			if downloads.Load() != 0 {
				t.Fatal("failed parent reached attachment download")
			}
			broken.Store(false)
			items, next, err = NewConnector().FetchIncremental(context.Background(), cfg, next)
			if err != nil {
				t.Fatalf("explicit retry failed: %v", err)
			}
			seen := map[string]bool{}
			for _, item := range items {
				if item.IsDeleted || seen[item.ExternalID] {
					t.Fatal("retry emitted deletion or duplicate")
				}
				seen[item.ExternalID] = true
			}
			if mode != "404" && (!seen["bad"] || !seen["good"]) {
				t.Fatal("retry did not complete both pages")
			}
			items, _, err = NewConnector().FetchIncremental(context.Background(), cfg, next)
			if err != nil || len(items) != 0 {
				t.Fatal("unchanged retry produced content again")
			}
			t.Logf("nested mode=%s: failed parent protected, sibling retained, explicit retry converged", mode)
		})
	}
}
