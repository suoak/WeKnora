package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type officeHTTPKB struct {
	interfaces.KnowledgeBaseService
}

func (officeHTTPKB) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: "kb-office-http", TenantID: 1, CreatorID: "owner", Type: types.KnowledgeBaseTypeDocument}, nil
}

// Real handler, error middleware and knowledge service. Authentication is a
// controlled incoming identity; this is not deployed JWT/provider acceptance.
func officeHTTPRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	on := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &on}}
	kb := officeHTTPKB{}
	ks, err := service.NewKnowledgeService(cfg, nil, nil, kb,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	h := &KnowledgeHandler{kgService: ks, kbService: kb}
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.Use(func(c *gin.Context) {
		user, role := "owner", types.TenantRoleAdmin
		if c.GetHeader("X-Synthetic-Denied") == "1" {
			user, role = "other", types.TenantRoleViewer
		}
		caller := types.Caller{TenantID: 1, UserID: user, Role: role}
		ctx := types.WithCaller(c.Request.Context(), caller)
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: user})
		ctx = context.WithValue(ctx, types.TenantIDContextKey, uint64(1))
		ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{ID: 1, StorageEngineConfig: &types.StorageEngineConfig{DefaultProvider: "local"}})
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Set(types.UserIDContextKey.String(), user)
		c.Set(types.TenantRoleContextKey.String(), role)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	// Use the production route's ownership and explicit write-grant guards;
	// a read-only handler resolution must never mint a write grant.
	router.POST("/api/v1/knowledge-bases/:id/knowledge/file",
		middleware.RequireOwnershipOrRole(types.TenantRoleAdmin, func(c *gin.Context) (string, error) {
			row, err := kb.GetKnowledgeBaseByID(c.Request.Context(), c.Param("id"))
			return row.CreatorID, err
		}, cfg),
		middleware.RequireKBAccess(middleware.KBIDFromParam("id"), types.OrgRoleEditor, kb, nil, nil, cfg),
		h.CreateKnowledgeFromFile)
	return router
}

func TestOfficeHTTPMultipartBypassReturns400And2400(t *testing.T) {
	router := officeHTTPRouter(t)
	for _, name := range []string{"encrypted.docx", "encrypted.xlsx", "encrypted.pptx", "encrypted.xlsm", "encrypted.doc", "encrypted.xls", "encrypted.ppt", "disguised.txt"} {
		t.Run(name, func(t *testing.T) {
			data, e := os.ReadFile("../officeguard/testdata/" + name)
			require.NoError(t, e)
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			part, e := form.CreateFormFile("file", name)
			require.NoError(t, e)
			_, e = part.Write(data)
			require.NoError(t, e)
			require.NoError(t, form.WriteField("fileName", "renamed.txt"))
			require.NoError(t, form.WriteField("channel", "confluence"))
			require.NoError(t, form.Close())
			for _, denied := range []bool{false, true} {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases/kb-office-http/knowledge/file", bytes.NewReader(body.Bytes()))
				req.Header.Set("Content-Type", form.FormDataContentType())
				if denied {
					req.Header.Set("X-Synthetic-Denied", "1")
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if denied {
					require.Equal(t, 403, w.Code, w.Body.String())
					continue
				}
				require.Equal(t, 400, w.Code, w.Body.String())
				var result struct{ Error struct{ Code int } }
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
				require.Equal(t, 2400, result.Error.Code)
			}
		})
	}
}

func TestOfficeHTTPUnsupportedWPSReturns400WithoutEncryptionCode(t *testing.T) {
	router := officeHTTPRouter(t)
	for _, name := range []string{"unknown.wps", "unknown.WPS"} {
		t.Run(name, func(t *testing.T) {
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			part, err := form.CreateFormFile("file", name)
			require.NoError(t, err)
			_, err = part.Write([]byte("WPS proprietary format: not an encrypted Office envelope"))
			require.NoError(t, err)
			require.NoError(t, form.Close())
			for _, denied := range []bool{false, true} {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge-bases/kb-office-http/knowledge/file", bytes.NewReader(body.Bytes()))
				req.Header.Set("Content-Type", form.FormDataContentType())
				if denied {
					req.Header.Set("X-Synthetic-Denied", "1")
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				if denied {
					require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
					continue
				}
				require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
				var result struct {
					Error struct {
						Code    int
						Message string
					}
				}
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
				require.Equal(t, 1000, result.Error.Code)
				require.Contains(t, result.Error.Message, "不支持")
				require.NotContains(t, result.Error.Message, "加密")
			}
		})
	}
}
