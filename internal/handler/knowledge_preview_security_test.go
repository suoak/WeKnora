package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type previewKnowledgeServiceStub struct {
	interfaces.KnowledgeService
	filename string
}

type exactPreviewKnowledgeService struct {
	interfaces.KnowledgeService
	called int
}

func (s *exactPreviewKnowledgeService) GetKnowledgeFile(ctx context.Context, id string) (io.ReadCloser, string, error) {
	s.called++
	if _, err := types.CheckExactPublishedDocumentRead(ctx); err != nil {
		return nil, "", err
	}
	return io.NopCloser(strings.NewReader("checked-docx-bytes")), "original.docx", nil
}

func TestPreviewExactPublishedFileRetainsViewerAndSourceChecks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"viewer", "missing-grant", "revoked", "foreign-id"} {
		t.Run(mode, func(t *testing.T) {
			svc := &exactPreviewKnowledgeService{}
			r := gin.New()
			r.Use(middleware.ErrorHandler())
			r.Use(func(c *gin.Context) {
				caller := types.Caller{TenantID: 42, UserID: "viewer", Role: types.TenantRoleViewer}
				ctx := types.WithExecutionTenant(types.WithCaller(c.Request.Context(), caller), 42)
				ctx = context.WithValue(
					ctx,
					types.PrincipalContextKey,
					types.Principal{Type: types.PrincipalWebUser, ID: "viewer"},
				)
				if mode != "missing-grant" {
					grant := access.KBAccess{
						Caller: caller, EffectiveTenantID: 42, Permission: types.OrgRoleViewer,
						KnowledgeBase: &types.KnowledgeBase{ID: "kb1", TenantID: 42},
					}
					ctx = grant.Context(ctx)
				}
				revoked := false
				ctx, err := types.WithExactPublishedDocumentRead(ctx, 1, types.ExactDocumentScope{
					TenantID: 42, KnowledgeID: "k1", KBID: "kb1", Read: func(context.Context) error {
						if revoked {
							return types.ErrExactDocumentScope
						}
						return nil
					},
				})
				require.NoError(t, err)
				revoked = mode == "revoked"
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			})
			h := &KnowledgeHandler{kgService: svc}
			r.GET("/knowledge/:id/preview", h.PreviewKnowledgeFile)
			id := "k1"
			if mode == "foreign-id" {
				id = "other"
			}
			w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder()}
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/knowledge/"+id+"/preview", nil))
			if mode == "viewer" {
				require.Equal(t, http.StatusOK, w.Code)
				require.Equal(t, "checked-docx-bytes", w.Body.String())
				require.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
			} else {
				require.Equal(t, http.StatusForbidden, w.Code)
				require.NotContains(t, w.Body.String(), "checked-docx-bytes")
			}
			if mode == "missing-grant" || mode == "foreign-id" {
				require.Zero(t, svc.called)
			}
		})
	}
}

type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool {
	ch := make(chan bool, 1)
	return ch
}

func (s *previewKnowledgeServiceStub) GetKnowledgeByIDOnly(context.Context, string) (*types.Knowledge, error) {
	return &types.Knowledge{ID: "k1", TenantID: 42, KnowledgeBaseID: "kb1"}, nil
}

func (s *previewKnowledgeServiceStub) GetKnowledgeFile(context.Context, string) (io.ReadCloser, string, error) {
	return io.NopCloser(strings.NewReader(`<html><script>alert(1)</script></html>`)), s.filename, nil
}

func TestPreviewKnowledgeFileForcesActiveContentDownload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.Use(func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(42))
		c.Next()
	})

	h := &KnowledgeHandler{kgService: &previewKnowledgeServiceStub{filename: "payload.html"}}
	router.GET("/knowledge/:id/preview", h.PreviewKnowledgeFile)

	req := httptest.NewRequest(http.MethodGet, "/knowledge/k1/preview", nil)
	w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder()}
	router.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
		t.Fatalf("Content-Disposition = %q, want attachment", got)
	}
}
