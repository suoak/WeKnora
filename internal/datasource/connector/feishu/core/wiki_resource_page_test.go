package core

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestWikiResourcePageShortcutIsExplicitBoundedAndIdentityBound(t *testing.T) {
	for _, scenario := range []string{"children", "cycle", "denied"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("SSRF_WHITELIST", "127.0.0.1,localhost")
			utils.ResetSSRFWhitelistForTest()
			t.Cleanup(utils.ResetSSRFWhitelistForTest)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, "Bearer user-only", r.Header.Get("Authorization"))
				switch r.URL.Path {
				case "/open-apis/wiki/v2/spaces/100/nodes":
					_, _ = fmt.Fprint(w, `{
  "code": 0,
  "data": {
    "items": [
      {
        "node_token": "alias",
        "title": "Shortcut",
        "obj_type": "docx",
        "node_type": "shortcut",
        "has_child": false
      }
    ]
  }
}`)
				case "/open-apis/wiki/v2/spaces/get_node":
					token := r.URL.Query().Get("token")
					if token == "alias" {
						_, _ = fmt.Fprint(w, `{
  "code": 0,
  "data": {
    "node": {
      "node_token": "alias",
      "space_id": "100",
      "node_type": "shortcut",
      "origin_node_id": "origin",
      "origin_space_id": "200"
    }
  }
}`)
					} else if scenario == "denied" {
						w.WriteHeader(http.StatusForbidden)
					} else if scenario == "cycle" {
						_, _ = fmt.Fprint(w, `{
  "code": 0,
  "data": {
    "node": {
      "node_token": "origin",
      "space_id": "200",
      "node_type": "shortcut",
      "origin_node_id": "alias",
      "origin_space_id": "100"
    }
  }
}`)
					} else {
						_, _ = fmt.Fprint(w, `{
  "code": 0,
  "data": {
    "node": {
      "node_token": "origin",
      "space_id": "200",
      "node_type": "origin",
      "has_child": true
    }
  }
}`)
					}
				case "/open-apis/wiki/v2/spaces/200/nodes":
					require.Equal(t, "origin", r.URL.Query().Get("parent_node_token"))
					require.Equal(t, "2", r.URL.Query().Get("page_size"))
					_, _ = fmt.Fprint(w, `{"code":0,"data":{"items":[{"node_token":"child","title":"Child","obj_type":"docx"}]}}`)
				default:
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			client := NewClient(&Config{AppID: "app", AppSecret: "no-fallback", BaseURL: server.URL})
			ctx := WithUserIdentity(context.Background(), func(context.Context) (string, error) { return "user-only", nil })
			root, err := client.ReadResourcePage(ctx, "wiki", "", "100", "", 2)
			require.NoError(t, err)
			require.EqualValues(t, 1, calls.Load(), "listing a page must not eagerly expand shortcuts")
			require.Len(t, root.Items, 1)
			require.True(t, root.Items[0].HasChildren)
			require.True(t, root.Items[0].IsShortcut)
			page, err := client.ReadResourcePage(ctx, "wiki", "alias", "100", "", 2)
			if scenario == "children" {
				require.NoError(t, err)
				require.Equal(t, "origin", page.ResolvedParent)
				require.Equal(t, "200", page.ResolvedSpace)
				require.Len(t, page.Items, 1)
				require.Equal(t, "child", page.Items[0].ID)
				require.EqualValues(t, 4, calls.Load())
			} else {
				require.Error(t, err)
				require.Empty(t, page.Items)
				require.EqualValues(t, 3, calls.Load(), "cycles and denied origins must stop without recursive retries")
			}
		})
	}
}
