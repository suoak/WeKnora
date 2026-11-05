package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type processingRouteKnowledge struct{ interfaces.KnowledgeService }

func (*processingRouteKnowledge) GetKnowledgeProcessingMetadata(
	ctx context.Context, id string,
) (*types.Knowledge, error) {
	if !types.IsKnowledgeProcessingMetadata(ctx, id) {
		return nil, apperrors.NewForbiddenError("No processing scope")
	}
	return &types.Knowledge{ID: id, TenantID: 1, KnowledgeBaseID: "kb", ParseStatus: "processing"}, nil
}

func (s *processingRouteKnowledge) GetKnowledgeByIDOnly(ctx context.Context, id string) (*types.Knowledge, error) {
	return s.GetKnowledgeProcessingMetadata(ctx, id)
}

type processingRouteKB struct {
	interfaces.KnowledgeBaseService
}

func (*processingRouteKB) GetKnowledgeBaseAccessMetadata(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: "kb", TenantID: 1, CreatorID: "owner"}, nil
}

func TestProcessingStatusRouteRetainsTenantAndKBGuardsWithoutPublishedBody(t *testing.T) {
	for _, tenant := range []uint64{1, 2} {
		enabled := true
		cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}
		kg, kb := &processingRouteKnowledge{}, &processingRouteKB{}
		h := handler.NewKnowledgeHandler(cfg, kg, kb, nil, nil, nil, nil, nil, nil, nil, nil)
		g := &rbacGuards{cfg: cfg, knowledgeService: kg, kbService: kb}
		r := gin.New()
		r.Use(middleware.ErrorHandler(), func(c *gin.Context) {
			ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, tenant)
			ctx = context.WithValue(ctx, types.UserIDContextKey, "owner")
			ctx = context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleViewer)
			ctx = types.WithCaller(ctx, types.Caller{TenantID: tenant, UserID: "owner", Role: types.TenantRoleViewer})
			c.Request = c.Request.WithContext(ctx)
			c.Next()
		})
		RegisterKnowledgeRoutes(r.Group("/api/v1"), h, g)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/doc/spans", nil))
		if tenant == 1 {
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"metadata_only":true`)
		} else {
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/knowledge/doc", nil))
		require.NotEqual(t, http.StatusOK, w.Code, "status scope must not reach ordinary content")
	}
}
