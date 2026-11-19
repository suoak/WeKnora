package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type workspaceDefaultsPolicy struct {
	interfaces.ModelPolicyService
	err    error
	writes int
}

func (p *workspaceDefaultsPolicy) GetWorkspacePolicy(context.Context) (*types.DefaultSettings, error) {
	return &types.DefaultSettings{ChatModelID: "configured"}, nil
}

func (p *workspaceDefaultsPolicy) UpdateWorkspacePolicy(
	context.Context,
	*types.DefaultSettings,
	*types.DefaultSettings,
) error {
	p.writes++
	return p.err
}

func TestWorkspaceDefaultsHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body     string
		err            error
		status, writes int
	}{
		{"save", `{"settings":{"chat_model_id":" chat "},"previous":{}}`, nil, 200, 1},
		{"missing previous", `{"settings":{}}`, nil, 400, 0},
		{"null settings", `{"settings":null,"previous":{}}`, nil, 400, 0},
		{"concurrent edit", `{"settings":{},"previous":{}}`, types.ErrDefaultSettingsConflict, 409, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := &workspaceDefaultsPolicy{err: tc.err}
			h := &TenantHandler{modelPolicy: policy}
			r := gin.New()
			r.PUT("/tenants/kv/:key", h.UpdateTenantKV)
			req := httptest.NewRequest(http.MethodPut, "/tenants/kv/default-settings", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.writes, policy.writes)
		})
	}
	policy := &workspaceDefaultsPolicy{}
	h := &TenantHandler{modelPolicy: policy}
	r := gin.New()
	r.GET("/tenants/kv/:key", h.GetTenantKV)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tenants/kv/default-settings", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"chat_model_id":"configured"`)
	require.NotContains(t, w.Body.String(), "api_key")
}
