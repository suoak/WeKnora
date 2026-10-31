package router

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type exactPublishedRouteService struct {
	interfaces.KnowledgeService
	allowed bool
}

func (s exactPublishedRouteService) ExactPublishedReadContext(
	ctx context.Context, id string,
) (context.Context, bool, error) {
	if !s.allowed || id != "published" {
		return ctx, true, types.ErrExactDocumentScope
	}
	ctx = types.WithExecutionTenant(ctx, 1)
	ctx, err := types.WithExactPublishedDocumentRead(ctx, 1, types.ExactDocumentScope{
		TenantID: 1, KnowledgeID: id, KBID: "kb",
		Read: func(context.Context) error { return nil },
	})
	return ctx, true, err
}

func TestExactPublishedReadRouteRetainsDocumentAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []string{"/knowledge/:id", "/chunks/:id"} {
		for _, allowed := range []bool{true, false} {
			t.Run(route+map[bool]string{true: "/viewer", false: "/denied"}[allowed], func(t *testing.T) {
				g := &rbacGuards{knowledgeService: exactPublishedRouteService{allowed: allowed}}
				r := gin.New()
				r.Use(middleware.ErrorHandler())
				called := false
				r.GET(route, g.DocumentReadFromKnowledge("id"), func(c *gin.Context) {
					called = true
					_, _, id, kb, ok := types.ExactPublishedDocumentIdentity(c.Request.Context())
					require.True(t, ok)
					require.Equal(t, "published", id)
					valid, err := types.CheckExactDocumentRead(c.Request.Context(), id, kb)
					require.True(t, valid)
					require.NoError(t, err)
					valid, err = types.CheckExactDocumentRead(c.Request.Context(), "old-held", kb)
					require.False(t, valid)
					require.NoError(t, err)
					c.JSON(200, gin.H{"success": true})
				})
				w := httptest.NewRecorder()
				path := "/knowledge/published"
				if route == "/chunks/:id" {
					path = "/chunks/published?page=1&page_size=25"
				}
				r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				require.Equal(t, allowed, called)
				if allowed {
					require.Equal(t, 200, w.Code)
				} else {
					require.Equal(t, 403, w.Code)
				}
			})
		}
	}
}
