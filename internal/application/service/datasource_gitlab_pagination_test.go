package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/datasource/connector/gitlab"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

// Uses the real connector and ProcessSync with the existing isolated repository
// doubles. No server/database credentials are read from deployment configuration.
func TestProcessSyncGitLabPaginationFailurePreservesCursor(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,::1,localhost")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/1":
			_, _ = w.Write([]byte(`{"id":1,"name":"docs","default_branch":"main"}`))
		case "/api/v4/projects/1/repository/commits/main":
			_, _ = w.Write([]byte(`{"id":"head"}`))
		case "/api/v4/projects/1/repository/tree":
			w.Header().Set("X-Next-Page", "2")
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	h := newSyncDeletionHarness(t, true, "gitlab-pagination-ds", "gitlab-pagination-log", nil, nil)
	config := &types.DataSourceConfig{
		Type:        types.ConnectorTypeGitLab,
		Credentials: map[string]interface{}{"base_url": server.URL, "access_token": "test-token"},
		Settings:    map[string]interface{}{"projects": []interface{}{map[string]interface{}{"project_id": "1"}}},
	}
	var err error
	h.ds.Config, err = config.ToJSON()
	require.NoError(t, err)
	h.ds.Type = types.ConnectorTypeGitLab
	old := &types.SyncCursor{ConnectorCursor: map[string]interface{}{"projects": map[string]string{"9": "retained"}}}
	h.ds.LastSyncCursor, err = old.ToJSON()
	require.NoError(t, err)
	before := string(h.ds.LastSyncCursor)
	registry := datasource.NewConnectorRegistry()
	require.NoError(t, registry.Register(gitlab.NewConnector()))
	h.svc.connectorRegistry = registry
	log, err := h.run(t)
	require.ErrorIs(t, err, asynq.SkipRetry)
	require.ErrorContains(t, err, "pagination repeated page")
	require.Equal(t, types.SyncLogStatusFailed, log.Status)
	require.Equal(t, types.DataSourceStatusError, h.ds.Status)
	require.Contains(t, log.ErrorMessage, "Fetch failed")
	require.Equal(t, before, string(h.ds.LastSyncCursor))
	require.Empty(t, h.knowledgeSvc.deleted, "pagination failure is not an authoritative deletion list")
}
