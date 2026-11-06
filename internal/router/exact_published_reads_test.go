package router

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type exactPublishedRouteService struct {
	interfaces.KnowledgeService
	allowed bool
	failure error
}

func (s exactPublishedRouteService) ExactPublishedReadContext(
	ctx context.Context, id string,
) (context.Context, bool, error) {
	if s.failure != nil {
		return ctx, true, s.failure
	}
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

func TestExactPublishedReadRouteReportsSourceRecoveryAction(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		reason string
	}{
		{"reconnect", service.ErrFeishuRefreshRejected, 403, "personal_feishu_reauthorization_required"},
		{"transient", core.ErrTransient, 503, "personal_feishu_verification_temporarily_unavailable"},
		{"rate_limit", core.ErrRateLimited, 503, "personal_feishu_verification_temporarily_unavailable"},
		{"deadline", context.DeadlineExceeded, 503, "personal_feishu_verification_temporarily_unavailable"},
		{"database", errors.New("private database error"), 500, "Document source verification failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &rbacGuards{knowledgeService: exactPublishedRouteService{failure: tc.err}}
			r := gin.New()
			r.Use(middleware.ErrorHandler())
			r.GET("/knowledge/:id", g.DocumentReadFromKnowledge("id"), func(c *gin.Context) { t.Fatal("denied source reached content handler") })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/knowledge/published", nil))
			require.Equal(t, tc.status, w.Code)
			require.Contains(t, w.Body.String(), tc.reason)
			require.NotContains(t, w.Body.String(), "private database error")
		})
	}
}

func TestExactPublishedReadRouteRetainsDocumentAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []string{"/knowledge/:id", "/chunks/:id", "/knowledge/:id/preview"} {
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
				switch route {
				case "/chunks/:id":
					path = "/chunks/published?page=1&page_size=25"
				case "/knowledge/:id/preview":
					path = "/knowledge/published/preview"
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
