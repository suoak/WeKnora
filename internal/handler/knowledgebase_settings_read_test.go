package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type settingsReadService struct {
	stubKBOnlyService
	projection *types.KnowledgeBase
}

func (s *settingsReadService) GetKnowledgeBaseAccessMetadata(
	context.Context,
	string,
) (*types.KnowledgeBase, error) {
	return s.projection, nil
}

func TestWorkspaceSettingsReadReloadsPersistedConfigAfterAccessProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	full := &types.KnowledgeBase{
		ID: "kb", TenantID: 7,
		VLMConfig:             types.VLMConfig{Enabled: true, ModelID: "vision", APIKey: "private-key"},
		ChunkingConfig:        types.ChunkingConfig{ChunkSize: 900, ChunkOverlap: 0},
		ImageProcessingConfig: types.ImageProcessingConfig{ImageAttrsEnabled: true},
	}
	loads := 0
	svc := &settingsReadService{projection: &types.KnowledgeBase{ID: "kb", TenantID: 7}}
	svc.getByID = func(context.Context, string) (*types.KnowledgeBase, error) { loads++; return full, nil }
	h := &KnowledgeBaseHandler{service: svc}
	r := gin.New()
	r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := types.WithCaller(
			c.Request.Context(),
			types.Caller{TenantID: 7, UserID: "member", Role: types.TenantRoleViewer},
		)
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "member"})
		c.Request = c.Request.WithContext(ctx)
		c.Set(types.TenantIDContextKey.String(), uint64(7))
	})
	r.GET("/kb/:id", h.GetKnowledgeBase)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/kb/kb", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, 1, loads)
	var body struct {
		Data types.KnowledgeBase `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Data.VLMConfig.Enabled)
	require.Equal(t, "vision", body.Data.VLMConfig.ModelID)
	require.Equal(t, 900, body.Data.ChunkingConfig.ChunkSize)
	require.Zero(t, body.Data.ChunkingConfig.ChunkOverlap)
	require.True(t, body.Data.ImageProcessingConfig.ImageAttrsEnabled)
	require.NotContains(t, w.Body.String(), "private-key")
}
