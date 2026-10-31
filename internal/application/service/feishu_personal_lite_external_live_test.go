//go:build anydoc && personal_exact_live

package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/router"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestPersonalOriginalExactLitePublicConsumer(t *testing.T) {
	service.RunOriginalExactQueueAcceptance(t, func(handler asynq.HandlerFunc) interfaces.TaskEnqueuer {
		executor := router.NewSyncTaskExecutor()
		executor.RegisterHandler(types.TypeDocumentProcess, handler)
		return executor
	}, func(ks interfaces.KnowledgeService, kbs interfaces.KnowledgeBaseService,
		viewer, stranger context.Context, kb, document string,
	) {
		probeOriginalExactHTTP(t, ks, kbs, viewer, stranger, kb, document)
	})
}

// Normal handlers and KB access middleware run against the real isolated DB.
// The injected principals represent already authenticated test users; this is
// not an OA/OIDC login or a production-session acceptance.
func probeOriginalExactHTTP(t *testing.T, ks interfaces.KnowledgeService,
	kbs interfaces.KnowledgeBaseService, viewer, stranger context.Context, kb, document string,
) {
	t.Helper()
	cfg := &config.Config{}
	documents := handler.NewKnowledgeHandler(cfg, ks, kbs, nil, nil, nil, nil, nil, nil, nil, nil)
	bases := handler.NewKnowledgeBaseHandler(cfg, kbs, ks, nil, nil, nil, nil, nil, nil, nil, nil)
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	guard := middleware.RequireKBAccess(middleware.KBIDFromParam("id"), types.OrgRoleViewer, kbs, nil, nil, cfg)
	engine.GET("/knowledge/:id", documents.GetKnowledge)
	engine.GET("/knowledge-bases/:id/knowledge", guard, documents.ListKnowledge)
	engine.POST("/knowledge-bases/:id/search", guard, bases.HybridSearch)
	for _, identity := range []struct {
		ctx     context.Context
		allowed bool
	}{{viewer, true}, {stranger, false}} {
		for _, route := range []struct{ method, path, body string }{
			{http.MethodGet, "/knowledge/" + document, ""},
			{http.MethodGet, "/knowledge-bases/" + kb + "/knowledge?page=1&page_size=10&keyword=original", ""},
			{
				http.MethodPost, "/knowledge-bases/" + kb + "/search",
				`{"query_text":"original","knowledge_ids":["` + document + `"],"match_count":10}`,
			},
		} {
			req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body)).
				WithContext(identity.ctx)
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			if identity.allowed {
				require.Equal(t, http.StatusOK, response.Code, "authorized normal HTTP path")
				var result struct {
					Success bool            `json:"success"`
					Data    json.RawMessage `json:"data"`
				}
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
				require.True(t, result.Success)
				require.NotEmpty(t, result.Data)
				require.NotEqual(t, "[]", string(result.Data))
			} else {
				require.GreaterOrEqual(t, response.Code, 400, "forged administrator/non-member must be rejected")
			}
		}
	}
}
