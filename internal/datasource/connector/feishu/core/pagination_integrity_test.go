package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// Every paginated reader must discard its prefix on malformed continuation or
// second-page failure. Exercise real HTTP decoding, query encoding and retries.
func TestPaginationIntegrityHTTP(t *testing.T) {
	for _, reader := range []string{"wiki_spaces", "wiki_nodes", "drive", "blocks", "fields", "records"} {
		for _, mode := range []string{
			"complete", "final_empty", "missing", "repeated", "empty", "403", "429", "500", "timeout",
		} {
			t.Run(reader+"/"+mode, func(t *testing.T) {
				const token = "next+&#%?"
				calls := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.Contains(r.URL.Path, "/auth/") {
						_ = json.NewEncoder(w).Encode(map[string]any{
							"code": 0, "tenant_access_token": "t", "expire": 7200,
						})
						return
					}
					// Bitable's other endpoint is a complete prerequisite/result.
					if (reader == "records" && strings.HasSuffix(r.URL.Path, "/fields")) ||
						(reader == "fields" && strings.HasSuffix(r.URL.Path, "/records/search")) {
						_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
							"items": []map[string]any{{"field_name": "column"}}, "has_more": false,
						}})
						return
					}
					calls++
					if reader == "wiki_nodes" {
						require.Equal(t, "parent+&#%?", r.URL.Query().Get("parent_node_token"))
					}
					if calls > 1 {
						require.Equal(t, token, r.URL.Query().Get("page_token"))
						switch mode {
						case "403":
							w.WriteHeader(403)
							return
						case "429":
							w.WriteHeader(429)
							return
						case "500":
							w.WriteHeader(500)
							return
						case "timeout":
							select {
							case <-r.Context().Done():
							case <-time.After(100 * time.Millisecond):
							}
							return
						}
					}
					items := []map[string]any{{
						"space_id": "space", "node_token": "node", "token": "file",
						"block_id": "block", "field_name": "column", "fields": map[string]any{"column": "value"},
					}}
					more, next := calls == 1, token
					if calls > 1 {
						next = ""
					}
					switch mode {
					case "missing":
						more, next = true, ""
					case "repeated":
						more, next = true, token
					case "empty":
						more, items = true, nil
					case "final_empty":
						more, items, next = false, nil, ""
					}
					data := map[string]any{
						"items": items, "files": items, "has_more": more, "page_token": next, "next_page_token": next,
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
				}))
				defer srv.Close()
				c := &Client{
					baseURL: srv.URL, appID: "a", appSecret: "s", httpClient: srv.Client(),
					retry: defaultRetryPolicy(),
				}
				c.retry.maxRetries = 0
				if mode == "timeout" {
					c.httpClient.Timeout = 30 * time.Millisecond
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				var count int
				var err error
				switch reader {
				case "wiki_spaces":
					v, e := c.ListWikiSpaces(ctx)
					count, err = len(v), e
				case "wiki_nodes":
					v, e := c.ListWikiNodes(ctx, "space/id", "parent+&#%?")
					count, err = len(v), e
				case "drive":
					v, e := c.ListDriveFilesAllPages(ctx, "folder")
					count, err = len(v), e
				case "blocks":
					v, e := c.listDocumentBlocks(ctx, "doc")
					count, err = len(v), e
				case "fields", "records":
					v, _, e := c.readBitableRecords(ctx, "app_table")
					count, err = len(v), e
				}
				if mode == "complete" || mode == "final_empty" {
					require.NoError(t, err)
					if mode == "complete" {
						require.GreaterOrEqual(t, count, 2)
					}
				} else {
					require.Error(t, err)
					require.Zero(t, count, "failed page must never return a complete-looking prefix")
				}
				require.LessOrEqual(t, calls, 2, "malformed pagination must be bounded")
			})
		}
	}
}

func TestPaginationContentLimits(t *testing.T) {
	for _, kind := range []string{"blocks", "fields", "records"} {
		for _, extra := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/exact", true: "/overflow"}[extra], func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.Contains(r.URL.Path, "/auth/") {
						_ = json.NewEncoder(w).Encode(map[string]any{
							"code": 0, "tenant_access_token": "t", "expire": 7200,
						})
						return
					}
					limit := maxTableRows
					if kind == "blocks" {
						limit = maxDocumentBlocks
					}
					if strings.HasSuffix(r.URL.Path, "/fields") {
						limit = 1
						if kind == "fields" {
							limit = maxBitableFields
						}
					}
					n := limit
					if extra && (kind != "records" || !strings.HasSuffix(r.URL.Path, "/fields")) {
						n++
					}
					items := make([]map[string]any, n)
					for i := range items {
						items[i] = map[string]any{
							"block_id": "b", "field_name": "column", "fields": map[string]any{"column": "value"},
						}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{
						"code": 0, "data": map[string]any{"items": items, "has_more": false},
					})
				}))
				defer srv.Close()
				c := &Client{baseURL: srv.URL, appID: "a", appSecret: "s", httpClient: srv.Client()}
				if kind == "blocks" {
					v, err := c.listDocumentBlocks(context.Background(), "doc")
					if extra {
						var cut *ContentTruncatedError
						require.ErrorAs(t, err, &cut)
						require.Nil(t, v)
					} else {
						require.NoError(t, err)
						require.Len(t, v, maxDocumentBlocks)
					}
				} else {
					v, cut, err := c.readBitableRecords(context.Background(), "app_table")
					if extra && kind == "fields" {
						var truncated *ContentTruncatedError
						require.True(t, errors.As(err, &truncated))
						require.Nil(t, v)
					} else {
						require.NoError(t, err)
						require.Equal(t, extra, cut)
					}
				}
			})
		}
	}
}

type paginationListOps struct{ deferralTestOps }

func (o *paginationListOps) List(ctx context.Context, c *Client, _ string) ([]string, error, error) {
	_, err := c.ListWikiNodes(ctx, "space", "")
	return nil, nil, err
}

func TestPaginationFailureCannotDeleteOrCheckpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0, "tenant_access_token": "t", "expire": 7200,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"items": []map[string]any{{"node_token": "new"}}, "has_more": true, "page_token": "",
		}})
	}))
	defer srv.Close()
	c := &Client{baseURL: srv.URL, appID: "a", appSecret: "s", httpClient: srv.Client()}
	ops := &paginationListOps{}
	initial := ops.EncodeCursor(map[string]map[string]string{"root": {"published-old": "old-version"}}, time.Now())
	before, err := json.Marshal(initial)
	require.NoError(t, err)
	h := &deferralTestHandler{}
	next, err := runSync(context.Background(), c, &types.DataSourceConfig{}, []string{"root"}, initial, h, ops)
	require.Error(t, err)
	require.Nil(t, next)
	require.Empty(t, h.items, "an incomplete listing cannot emit a deletion or replacement")
	require.Empty(t, h.checkpoints, "an incomplete listing cannot acknowledge a source version")
	after, err := json.Marshal(initial)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
	// A different source can still sync with its own execution and cursor.
	good := &deferralTestOps{nodes: []string{"healthy"}}
	_, err = runSync(context.Background(), &Client{}, &types.DataSourceConfig{},
		[]string{"other"}, nil, &deferralTestHandler{}, good)
	require.NoError(t, err)
	require.Equal(t, []string{"healthy"}, good.fetched)
}
