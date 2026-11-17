package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestResourcePageKeepsLegacyArrayAndValidatesOptions(t *testing.T) {
	calls := 0
	var captured types.ResourceListOptions
	service := &stubDataSourceService{
		getDataSource: func(context.Context, string) (*types.DataSource, error) {
			return &types.DataSource{KnowledgeBaseID: "kb"}, nil
		},
		listResources: func(context.Context, string, string) ([]types.Resource, error) { calls++; return nil, nil },
		listPage: func(_ context.Context, _ string, options types.ResourceListOptions) (*types.ResourcePage, error) {
			calls++
			captured = options
			if options.Limit != 50 {
				t.Errorf("default limit=%d", options.Limit)
			}
			return &types.ResourcePage{NextCursor: "next"}, nil
		},
	}
	kbService := &stubKBServiceForDS{getByID: func(context.Context, string) (*types.KnowledgeBase, error) {
		return &types.KnowledgeBase{ID: "kb", TenantID: 1}, nil
	}}
	router := newDataSourceTestRouter(NewDataSourceHandler(service, kbService))
	for _, tc := range []struct {
		query  string
		tenant uint64
		status int
		body   string
	}{
		{"", 1, 200, "[]"},
		{"?paged=true", 1, 200, `{"items":[],"next_cursor":"next"}`},
		{"?paged=true&limit=0", 1, 400, ""},
		{"?paged=true&limit=101", 1, 400, ""},
		{"?paged=true&limit=no", 1, 400, ""},
		{"?paged=true&parent_id=1&q=query", 1, 400, ""},
		{"?paged=true", 2, 403, ""},
		{"?paged=true", 0, 401, ""},
		{"?paged=true&reveal_id=page%3A1%3A30&cursor=token", 1, 400, ""},
		{"?paged=true&reveal_id=page%3A1%3A30&parent_id=1", 1, 400, ""},
		{"?paged=true&parent_id=1&space_key=ENG&cursor=token", 1, 200, `{"items":[],"next_cursor":"next"}`},
		{"?paged=true&reveal_id=page%3A1%3A30", 1, 200, `{"items":[],"next_cursor":"next"}`},
	} {
		before := calls
		w := httptest.NewRecorder()
		router.ServeHTTP(
			w,
			withDSCtx(httptest.NewRequest(http.MethodGet, "/datasource/id/resources"+tc.query, nil), tc.tenant),
		)
		if w.Code != tc.status || tc.body != "" && strings.TrimSpace(w.Body.String()) != tc.body {
			t.Errorf("%s: status=%d body=%s", tc.query, w.Code, w.Body.String())
		}
		if tc.status != 200 && calls != before {
			t.Error("invalid/unauthorized request reached connector")
		}
		if strings.Contains(tc.query, "space_key=ENG") &&
			(captured.ParentID != "1" || captured.SpaceKey != "ENG" || captured.Cursor != "token") {
			t.Errorf("lost child page options: %#v", captured)
		}
		if tc.status == 200 && strings.Contains(tc.query, "reveal_id=") && captured.RevealID != "page:1:30" {
			t.Errorf("lost reveal option: %#v", captured)
		}
	}
}
