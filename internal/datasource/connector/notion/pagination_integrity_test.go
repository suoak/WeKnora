package notion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestNotionPaginationIntegrityAndControlledErrors(t *testing.T) {
	for _, endpoint := range []string{"flat", "recursive", "search", "query"} {
		for _, shape := range []string{"repeated", "empty-cursor", "empty-page"} {
			t.Run(endpoint+"/"+shape, func(t *testing.T) {
				var calls atomic.Int64
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					n := calls.Add(1)
					if n > 3 {
						w.WriteHeader(400)
						return
					}
					results := `[{"id":"synthetic","object":"page","type":"paragraph"}]`
					cursor := "PROVIDER_CURSOR_CANARY"
					if shape == "empty-page" {
						results = `[]`
					}
					if shape == "empty-cursor" {
						cursor = ""
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprintf(w, `{"results":%s,"has_more":true,"next_cursor":%q}`, results, cursor)
				}))
				defer srv.Close()
				client := mustTestClient(t, "test-token", srv.URL)
				client.limiter = rate.NewLimiter(rate.Inf, 1)
				var err error
				switch endpoint {
				case "flat":
					_, err = client.GetBlockChildrenFlat(context.Background(), "synthetic")
				case "recursive":
					_, err = client.GetBlockChildrenAll(context.Background(), "synthetic")
				case "search":
					_, err = client.SearchPages(context.Background())
				case "query":
					_, err = client.QueryDatabaseAll(context.Background(), "synthetic")
				}
				require.Error(t, err)
				require.NotContains(t, err.Error(), "PROVIDER_CURSOR_CANARY")
				expected := int64(1)
				if shape == "repeated" || (endpoint == "search" && shape == "empty-page") {
					// Search permits an empty intermediate page. This fixture
					// repeats its cursor, so the second page must still fail.
					expected = 2
				}
				require.Equal(t, expected, calls.Load())
			})
		}
	}
}

func TestNotionSearchEmptyIntermediatePageCompletes(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			_, _ = io.WriteString(w, `{"results":[],"has_more":true,"next_cursor":"second-page"}`)
		case 2:
			_, _ = io.WriteString(w, `{"results":[{"id":"synthetic-page","object":"page"}],"has_more":false}`)
		default:
			t.Error("unexpected extra request")
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	client := mustTestClient(t, "test-token", srv.URL)
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	pages, err := client.SearchPages(context.Background())
	require.NoError(t, err)
	require.Len(t, pages, 1)
	require.Equal(t, "synthetic-page", pages[0].ID)
	require.Equal(t, int64(2), calls.Load())
}

func TestNotionBlockCapRejectsIncompleteAndKeepsExactComplete(t *testing.T) {
	for _, shape := range []string{"exact-complete", "cap-incomplete", "over-cap"} {
		t.Run(shape, func(t *testing.T) {
			count := maxBlocksPerPage
			if shape == "over-cap" {
				count++
			}
			blocks := make([]map[string]any, count)
			for i := range blocks {
				blocks[i] = map[string]any{"id": fmt.Sprint(i), "type": "paragraph"}
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"results": blocks, "has_more": shape == "cap-incomplete", "next_cursor": "next"})
			}))
			defer srv.Close()
			result, err := mustTestClient(t, "test-token", srv.URL).GetBlockChildrenAll(context.Background(), "synthetic")
			if shape == "exact-complete" {
				require.NoError(t, err)
				require.Len(t, result, maxBlocksPerPage)
			} else {
				require.ErrorIs(t, err, errQueryResultTruncated)
				require.Nil(t, result)
			}
		})
	}
}

func TestNotionPaginationLimitNeverEchoesCursor(t *testing.T) {
	_, err := advancePaginationCursor(context.Background(), map[string]struct{}{}, "PROVIDER_CURSOR_CANARY", maxPaginationHops)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "PROVIDER_CURSOR_CANARY")
}

func TestNotionMalformedTimestampDoesNotExposeProviderValue(t *testing.T) {
	for _, endpoint := range []string{"page", "search", "query"} {
		t.Run(endpoint, func(t *testing.T) {
			logs := captureNotionLogs(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p := map[string]any{"id": "synthetic", "object": "page", "created_time": "PROVIDER_TIME_CANARY"}
				if endpoint == "page" {
					_ = json.NewEncoder(w).Encode(p)
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{p}, "has_more": false})
				}
			}))
			defer srv.Close()
			client := mustTestClient(t, "test-token", srv.URL)
			var err error
			switch endpoint {
			case "page":
				_, err = client.GetPage(context.Background(), "synthetic")
			case "search":
				_, err = client.SearchPages(context.Background())
			case "query":
				_, err = client.QueryDatabaseAll(context.Background(), "synthetic")
			}
			require.Error(t, err)
			for cause := err; cause != nil; cause = errors.Unwrap(cause) {
				require.NotContains(t, fmt.Sprintf("%+v", cause), "PROVIDER_TIME_CANARY")
			}
			require.NotContains(t, logs.String(), "PROVIDER_TIME_CANARY")
		})
	}
}

func TestNotionPaginationHopLimitRealHTTP(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		_, _ = fmt.Fprintf(w, `{"results":[{"id":"synthetic","object":"page"}],"has_more":true,"next_cursor":"synthetic-%d"}`, n)
	}))
	defer srv.Close()
	client := mustTestClient(t, "test-token", srv.URL)
	client.limiter = rate.NewLimiter(rate.Inf, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pages, err := client.SearchPages(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pagination exceeded 10000 pages")
	require.Equal(t, int64(maxPaginationHops), calls.Load())
	require.Nil(t, pages)
}

func TestNotionIncompleteDatabaseRetainsWholeCursorAndRetries(t *testing.T) {
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
			bad := page("bad")
			bad["object"] = "data_source"
			body = map[string]any{"results": []any{bad, page("good")}, "has_more": false}
		case "/v1/data_sources/bad":
			body = map[string]any{"id": "bad", "object": "data_source"}
		case "/v1/data_sources/bad/query":
			body = map[string]any{"results": []any{page("record"), page("historical-record")}, "has_more": false}
			if broken.Load() {
				body.(map[string]any)["request_status"] = map[string]any{"type": "incomplete", "incomplete_reason": "PROVIDER_REASON_CANARY"}
			}
		case "/v1/blocks/good/children", "/v1/blocks/record/children", "/v1/blocks/historical-record/children":
			body = map[string]any{"results": []any{}, "has_more": false}
		default:
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()
	cfg := makeNotionConfig(&Config{APIKey: "synthetic-key"}, srv.URL, []string{"bad", "good"})
	prior := map[string]time.Time{"bad": old, "good": old, "historical-record": old}
	items, next, err := NewConnector().FetchIncremental(context.Background(), cfg, buildCursor(prior))
	var partial *datasource.PartialFetchError
	require.True(t, errors.As(err, &partial))
	require.NotContains(t, err.Error(), "PROVIDER_REASON_CANARY")
	require.NotNil(t, next)
	var decoded notionCursor
	b, _ := json.Marshal(next.ConnectorCursor)
	require.NoError(t, json.Unmarshal(b, &decoded))
	require.Equal(t, prior, decoded.PageEditTimes)
	good := false
	for _, item := range items {
		require.False(t, item.IsDeleted)
		require.NotEqual(t, "bad", item.ExternalID)
		if item.ExternalID == "good" {
			good = true
		}
	}
	require.True(t, good)
	broken.Store(false)
	items, _, err = NewConnector().FetchIncremental(context.Background(), cfg, next)
	require.NoError(t, err)
	require.NotEmpty(t, items)
	for _, item := range items {
		require.False(t, item.IsDeleted)
		require.False(t, strings.Contains(string(item.Content), "PROVIDER_REASON_CANARY"))
	}
}
