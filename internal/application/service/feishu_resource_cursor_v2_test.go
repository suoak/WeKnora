package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuV2PostgresResourceCursorRejectsMovedShortcut(t *testing.T) {
	f := v2PostgresFixture(t)
	var origin atomic.Value
	origin.Store("origin")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer access-A", r.Header.Get("Authorization"))
		data := map[string]any{}
		if r.URL.Path == "/open-apis/wiki/v2/spaces/get_node" {
			token := r.URL.Query().Get("token")
			node := map[string]any{"node_token": token, "space_id": "200", "node_type": "origin"}
			if token == "alias" {
				node["space_id"], node["node_type"] = "100", "shortcut"
				node["origin_space_id"], node["origin_node_id"] = "200", origin.Load()
			}
			data["node"] = node
		} else {
			require.Equal(t, "/open-apis/wiki/v2/spaces/200/nodes", r.URL.Path)
			data["items"] = []map[string]any{{"node_token": "child", "obj_type": "docx", "title": "Controlled"}}
			data["has_more"] = r.URL.Query().Get("page_token") == ""
			data["page_token"] = "next"
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data}))
	}))
	defer server.Close()
	f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	_, _, ref, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	query := FeishuResourceQuery{
		AuthMode: types.FeishuUserOAuth, ConnectionID: ref.ConnectionID,
		Kind: "wiki", Parent: "alias", Space: "100", Size: 2,
	}
	page, err := f.s.Resources(f.ctx, query)
	require.NoError(t, err)
	require.NotEmpty(t, page.Cursor)
	query.Cursor = page.Cursor
	origin.Store("moved")
	next, err := f.s.Resources(f.ctx, query)
	require.ErrorContains(t, err, "resource_parent_changed")
	require.Empty(t, next.Items, "a moved shortcut must not merge pages from different origins")
}
