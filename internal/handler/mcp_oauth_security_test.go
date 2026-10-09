package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/mcp"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type securityOAuthService struct {
	interfaces.MCPServiceService
	service *types.MCPService
}

func (s securityOAuthService) GetMCPServiceByID(context.Context, uint64, string) (*types.MCPService, error) {
	return s.service, nil
}

type securityOAuthRepo struct {
	interfaces.MCPOAuthRepository
	fail bool
}

func (s securityOAuthRepo) GetClient(context.Context, uint64, string) (*types.MCPOAuthClient, error) {
	if s.fail {
		return nil, errors.New("SQL client_secret=synthetic-secret access_token=synthetic-access refresh_token=synthetic-refresh")
	}
	return nil, nil
}
func (s securityOAuthRepo) GetTokenForPrincipal(context.Context, uint64, types.Principal, string) (*types.MCPOAuthToken, error) {
	return nil, errors.New("SQL synthetic-secret synthetic-access synthetic-refresh")
}
func (s securityOAuthRepo) DeleteTokenForPrincipal(context.Context, uint64, types.Principal, string) error {
	return errors.New("SQL synthetic-secret synthetic-access synthetic-refresh")
}

func TestOAuthAPIAndAccessLogsDoNotLeakProviderOrSQL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	utils.SetSSRFWhitelistFromRaw("127.0.0.1")
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	const sensitive = "synthetic-secret synthetic-access synthetic-refresh"
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/register" {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_client", "error_description": sensitive})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": provider.URL, "registration_endpoint": provider.URL + "/register", "token_endpoint": provider.URL + "/token", "authorization_endpoint": provider.URL + "/authorize"})
	}))
	t.Cleanup(provider.Close)
	u := provider.URL + "/mcp"
	service := &types.MCPService{ID: "svc", URL: &u, AuthConfig: &types.MCPAuthConfig{AuthType: types.MCPAuthOAuth, AuthServerMetadataURL: provider.URL + "/metadata"}}
	for _, path := range []string{"provider", "sql", "callback", "status", "revoke"} {
		t.Run(path, func(t *testing.T) {
			var captured bytes.Buffer
			l := logrus.New()
			l.SetOutput(&captured)
			ctx := types.WithPrincipal(types.WithExecutionTenant(context.Background(), 7), types.Principal{Type: types.PrincipalWebUser, ID: "user"})
			ctx = context.WithValue(ctx, types.LoggerContextKey, logrus.NewEntry(l))
			router := gin.New()
			router.ContextWithFallback = true
			router.Use(middleware.Logger(), middleware.ErrorHandler())
			router.Use(func(c *gin.Context) { c.Set(types.TenantIDContextKey.String(), uint64(7)); c.Next() })
			manager := mcp.NewOAuthManager(securityOAuthRepo{fail: path == "sql"}, nil, nil)
			h := NewMCPOAuthHandler(manager, nil, securityOAuthService{service: service}, nil)
			router.POST("/authorize/:id", h.AuthorizeURL)
			router.GET("/callback", h.Callback)
			router.GET("/status/:id", h.Status)
			router.DELETE("/revoke/:id", h.Revoke)
			var req *http.Request
			switch path {
			case "callback":
				req = httptest.NewRequest("GET", "/callback?error="+url.QueryEscape(sensitive)+"&error_description="+url.QueryEscape(sensitive)+"&client_secret=synthetic-secret", nil)
			case "status":
				req = httptest.NewRequest("GET", "/status/svc", nil)
			case "revoke":
				req = httptest.NewRequest("DELETE", "/revoke/svc", nil)
			default:
				req = httptest.NewRequest("POST", "/authorize/svc", strings.NewReader(`{"redirect_uri":"http://localhost/callback"}`))
				req.Header.Set("Content-Type", "application/json")
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req.WithContext(ctx))
			if path == "callback" {
				require.Equal(t, 302, recorder.Code)
				require.Contains(t, recorder.Header().Get("Location"), "provider_error")
			} else {
				require.Equal(t, 500, recorder.Code)
				require.Contains(t, recorder.Body.String(), "failed")
			}
			require.NotEmpty(t, captured.String())
			for _, s := range []string{"synthetic-secret", "synthetic-access", "synthetic-refresh"} {
				require.NotContains(t, recorder.Body.String(), s)
				require.NotContains(t, recorder.Header().Get("Location"), s)
				require.NotContains(t, captured.String(), s)
			}
		})
	}
}
