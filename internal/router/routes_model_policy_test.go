package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type routeModelPolicy struct{ interfaces.ModelPolicyService }

func (*routeModelPolicy) ResolveChatModelID(context.Context) string      { return "" }
func (*routeModelPolicy) ResolveSummaryModelID(context.Context) string   { return "" }
func (*routeModelPolicy) ResolveEmbeddingModelID(context.Context) string { return "" }
func (*routeModelPolicy) ResolveRerankModelID(context.Context) string    { return "" }
func (*routeModelPolicy) ResolveVLMModelID(context.Context) string       { return "" }
func (*routeModelPolicy) ResolveASRModelID(context.Context) string       { return "" }

func TestDefaultModelPolicyRouteHitsStaticHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	guards := &rbacGuards{}
	modelHandler := handler.NewModelHandler(nil, &routeModelPolicy{})
	RegisterModelRoutes(engine.Group("/api/v1"), modelHandler, &handler.ModelCredentialsHandler{}, guards)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/models/default-policy", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, `{"chat":null,"summary":null,"embedding":null,"rerank":null,"vlm":null,"asr":null}`, response.Body.String())
}

func TestSystemAdminModelPolicyRoutesAreExplicit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	guards := &rbacGuards{}
	RegisterSystemAdminRoutes(engine.Group("/api/v1"), &handler.SystemHandler{}, nil, guards)

	want := map[string]string{
		http.MethodGet + " /api/v1/system/admin/model-policy": "GetModelPolicy",
		http.MethodPut + " /api/v1/system/admin/model-policy": "UpdateModelPolicy",
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if handlerName, ok := want[key]; ok {
			assert.True(t, strings.Contains(route.Handler, handlerName), "%s resolved to %s", key, route.Handler)
			delete(want, key)
		}
	}
	require.Empty(t, want, "model-policy routes must be registered as explicit static paths")
}
