package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuBrowseUserIsolationAndDisconnectDuringListing(t *testing.T) {
	s, _ := feishuFixture(t)
	for _, user := range []string{"A", "B"} {
		encrypted, err := sealFeishu(feishuTokens{UserID: user, Access: "grant-" + user, Refresh: "refresh-" + user})
		require.NoError(t, err)
		require.NoError(t, s.db.Create(&types.FeishuConnection{UserID: user, AppID: "cli_test", OpenID: "open-" + user, Tokens: encrypted, ExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(time.Hour)}).Error)
	}
	disconnect := false
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "/open-apis/drive/v1/files", r.URL.Path)
		user := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer grant-")
		require.Contains(t, []string{"A", "B"}, user)
		if disconnect {
			require.NoError(t, s.Disconnect(personalUser(user)))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"files": []map[string]string{{"token": "doc_" + user, "name": "Synthetic " + user, "type": "docx", "url": "https://team.feishu.cn/docx/doc_" + user}}, "has_more": false}})
	}))
	defer server.Close()
	s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	for _, user := range []string{"A", "B"} {
		page, err := s.Browse(personalUser(user), "", "")
		require.NoError(t, err)
		require.Len(t, page.Items, 1)
		require.Equal(t, "doc_"+user, page.Items[0].Token)
	}
	before := calls
	_, err := s.Browse(context.Background(), "", "")
	require.Error(t, err)
	require.Equal(t, before, calls)
	disconnect = true
	_, err = s.Browse(personalUser("A"), "", "")
	require.ErrorIs(t, err, ErrFeishuReconnect)
	before = calls
	_, err = s.Browse(personalUser("A"), "", "")
	require.ErrorIs(t, err, ErrFeishuReconnect)
	require.Equal(t, before, calls)
	var imports int64
	require.NoError(t, s.db.Model(&types.FeishuPersonalImport{}).Count(&imports).Error)
	require.Zero(t, imports, "browsing must never create imports or KB records")
}
