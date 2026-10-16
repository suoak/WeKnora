package handler

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/testutil/privatefile"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type localBrowserTenants struct{ interfaces.TenantService }

func (localBrowserTenants) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	if id != 42 && id != 43 {
		return nil, fmt.Errorf("synthetic space not found")
	}
	return &types.Tenant{ID: id, Name: "Synthetic existing space"}, nil
}

type localBrowserMembers struct {
	interfaces.TenantMemberService
	db *gorm.DB
}

func (m localBrowserMembers) GetMembership(ctx context.Context, user string, tenant uint64) (*types.TenantMember, error) {
	var row types.TenantMember
	err := m.db.WithContext(ctx).Where("user_id = ? AND tenant_id = ?", user, tenant).First(&row).Error
	return &row, err
}
func (m localBrowserMembers) ListByUser(ctx context.Context, user string) ([]*types.TenantMember, error) {
	var rows []*types.TenantMember
	err := m.db.WithContext(ctx).Where("user_id = ?", user).Find(&rows).Error
	return rows, err
}

// Real Vue frontend + real auth handlers/service/repositories. Other Portal
// APIs use bounded fixture responses; this is not an LLM/RAG ingestion test.
func TestSourceIDLocalBrowserBusinessLogin(t *testing.T) {
	runSourceIDLocalBusinessLogin(t, false, true, "contributor")
}

func TestSourceIDSSOBrowserBusinessLogin(t *testing.T) {
	runSourceIDLocalBusinessLogin(t, true, true, "contributor")
}

func TestSourceIDSSOAccessOnlyHTTP(t *testing.T) {
	for _, role := range []string{"viewer", "contributor", "owner", "tenant_admin", "org_admin", "system_admin", "cross_space_admin", "multi_space"} {
		t.Run(role, func(t *testing.T) { runSourceIDLocalBusinessLogin(t, true, false, role) })
	}
}

func runSourceIDLocalBusinessLogin(t *testing.T, ordinary, browser bool, role string) {
	if browser && os.Getenv("SOURCEID_BROWSER_E2E") != "1" {
		t.Skip("opt-in synthetic browser integration")
	}
	t.Setenv("JWT_SECRET", "synthetic-browser-test-signing-key")
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "browser.db")), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.AuthToken{}, &types.TenantMember{}))
	require.NoError(t, db.Exec("DROP INDEX IF EXISTS idx_auth_tokens_session_id").Error)
	require.NoError(t, db.Exec("ALTER TABLE auth_tokens DROP COLUMN session_id").Error)
	for _, name := range []string{"003024_sourceid_oidc", "003026_sourceid_binding_review", "003027_sourceid_local_session"} {
		data, err := os.ReadFile(filepath.Join(root, "migrations/sqlite/"+name+".up.sql"))
		require.NoError(t, err)
		require.NoError(t, db.Exec(string(data)).Error)
	}
	require.NoError(t, db.Exec("CREATE TABLE organization_tenant_members(tenant_id BIGINT,role TEXT,representative_user_id TEXT)").Error)
	require.NoError(t, db.Create(&types.Tenant{ID: 42, Name: "Synthetic existing space"}).Error)
	user := types.User{ID: uuid.NewString(), Username: "synthetic-ordinary", Email: "ordinary@example.test", TenantID: 42, IsActive: true}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&types.TenantMember{UserID: user.ID, TenantID: 42, Role: types.TenantRoleContributor, Status: types.TenantMemberStatusActive, JoinedAt: time.Now()}).Error)
	switch role {
	case "viewer", "owner":
		require.NoError(t, db.Model(&types.TenantMember{}).Where("user_id=?", user.ID).Update("role", role).Error)
	case "tenant_admin":
		require.NoError(t, db.Model(&types.TenantMember{}).Where("user_id=?", user.ID).Update("role", "admin").Error)
	case "org_admin":
		require.NoError(t, db.Exec("INSERT INTO organization_tenant_members VALUES (42,'admin',?)", user.ID).Error)
	case "system_admin":
		user.IsSystemAdmin = true
		require.NoError(t, db.Save(&user).Error)
	case "cross_space_admin":
		user.CanAccessAllTenants = true
		require.NoError(t, db.Save(&user).Error)
	case "multi_space":
		require.NoError(t, db.Create(&types.Tenant{ID: 43, Name: "Synthetic second space"}).Error)
		require.NoError(t, db.Create(&types.TenantMember{UserID: user.ID, TenantID: 43, Role: types.TenantRoleViewer, Status: types.TenantMemberStatusActive, JoinedAt: time.Now()}).Error)
	}
	var originalMembers []types.TenantMember
	require.NoError(t, db.Order("tenant_id").Find(&originalMembers).Error)
	require.NoError(t, db.Exec("CREATE TABLE synthetic_knowledge_bases(id TEXT PRIMARY KEY,tenant_id BIGINT,name TEXT)").Error)
	require.NoError(t, db.Exec("INSERT INTO synthetic_knowledge_bases VALUES ('synthetic-kb',42,'Synthetic original knowledge base')").Error)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var enabled atomic.Bool
	enabled.Store(true)
	var tokens atomic.Int64
	var codes sync.Map
	cfg := &config.OIDCAuthConfig{Enable: true, SourceIDEnable: true, SourceIDContractConfirmed: true,
		SourceIDDiagnosticsEnable: true,
		SourceIDSessionMode:       "local_approved", SourceIDEmployeeClaim: "attributes.RJGH", SourceIDClaimsSource: "userinfo",
		SourceIDEmailBindingEnable: true, SourceIDEmailContractConfirmed: true, SourceIDAutoBindingEnable: true,
		SourceIDAutoBindingPolicyRef: "synthetic-binding-policy", SourceIDLocalPolicyRef: "synthetic-local-approval",
		SourceIDAdmissionMode: "authorization", SourceIDAdmissionPolicyRef: "synthetic-sid-authorization-contract",
		SourceIDAuthorizationRecheckConfirmed: true, SourceIDLocalAccessTTL: "10m", SourceIDLocalAbsoluteTTL: "10m",
		SourceIDForceReauthPromptLogin: true,
		SourceIDLocalIdleTTL:           "10m", SourceIDAdmissionMaxAge: "10m", ClientID: "knowhub", ClientSecret: "synthetic-secret",
		IssuerURL: "https://mock.example", ClientAuthMethod: "client_secret_basic", UserInfoTransport: "bearer_get",
		Scopes: []string{"openid"}, RedirectURI: "http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback", ProviderDisplayName: "OA"}
	if ordinary {
		cfg.SourceIDAdmissionMode = "oidc_sso_approved"
		cfg.SourceIDSSOPolicyConfirmed = true
		cfg.SourceIDForceReauthPromptLogin = false
		cfg.SourceIDAuthorizationRecheckConfirmed = false
		cfg.SourceIDLocalAccessTTL = "60m"
		cfg.SourceIDLocalAbsoluteTTL = "60m"
		cfg.SourceIDLocalIdleTTL = "30m"
		cfg.SourceIDAdmissionMaxAge = "60m"
		cfg.SourceIDPilotAccountsFile = filepath.Join(t.TempDir(), "ownership.json")
		manifest, _ := json.Marshal(map[string]interface{}{"version": 1, "policy_ref": cfg.SourceIDAutoBindingPolicyRef, "ownership_ref": "synthetic-snapshot-ownership", "accounts": []map[string]string{{"user_id": user.ID, "email": user.Email}}})
		require.NoError(t, privatefile.WriteFile(cfg.SourceIDPilotAccountsFile, manifest, 0600))
	}
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			q := r.URL.Query()
			if ordinary {
				require.False(t, q.Has("prompt"))
			} else {
				require.Equal(t, []string{"login"}, q["prompt"])
			}
			require.False(t, q.Has("max_age"))
			dest, _ := url.Parse(q.Get("redirect_uri"))
			values := dest.Query()
			values.Set("state", q.Get("state"))
			if !enabled.Load() {
				values.Set("error", "access_denied")
			} else {
				code := uuid.NewString()
				codes.Store(code, q.Get("nonce"))
				values.Set("code", code)
			}
			dest.RawQuery = values.Encode()
			http.Redirect(w, r, dest.String(), http.StatusFound)
		case "/token":
			tokens.Add(1)
			_ = r.ParseForm()
			client, secret, ok := r.BasicAuth()
			if !ok || client != "knowhub" || secret != "synthetic-secret" {
				w.WriteHeader(401)
				return
			}
			nonce, ok := codes.LoadAndDelete(r.Form.Get("code"))
			if !ok {
				w.WriteHeader(400)
				return
			}
			if r.Form.Get("redirect_uri") != cfg.RedirectURI || r.Form.Get("code_verifier") != "" || r.Form.Get("client_secret") != "" {
				w.WriteHeader(400)
				return
			}
			claims := jwt.MapClaims{"iss": cfg.IssuerURL, "aud": "knowhub", "sub": "synthetic-sub", "nonce": nonce, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix()}
			signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
			if err != nil {
				w.WriteHeader(500)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "synthetic-sid-access", "id_token": signed, "token_type": "Bearer"})
		case "/jwks":
			enc := base64.RawURLEncoding.EncodeToString
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": []map[string]string{{"kty": "RSA", "use": "sig", "alg": "RS256", "n": enc(key.N.Bytes()), "e": enc(big.NewInt(int64(key.E)).Bytes())}}})
		case "/userinfo":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"sub": "synthetic-sub", "attributes": map[string]string{"RJGH": "synthetic-employee", "RJEMAIL": "ordinary@example.test"}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer mock.Close()
	cfg.AuthorizationEndpoint = mock.URL + "/authorize"
	cfg.TokenEndpoint = mock.URL + "/token"
	cfg.JwksURI = mock.URL + "/jwks"
	cfg.UserInfoEndpoint = mock.URL + "/userinfo"
	appCfg := &config.Config{OIDCAuth: cfg, Auth: &config.AuthConfig{RegistrationMode: "invite_only"}, Tenant: &config.TenantConfig{}}
	tenants := localBrowserTenants{}
	users := service.NewUserService(appCfg, apprepo.NewUserRepository(db), apprepo.NewAuthTokenRepository(db), tenants, localBrowserMembers{db: db}, nil)
	h := NewAuthHandler(appCfg, users, tenants, nil, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(errorCapture())
	router.GET("/api/v1/auth/oidc/config", h.GetOIDCConfig)
	router.GET("/api/v1/auth/config", h.GetAuthConfig)
	router.GET("/api/v1/auth/oidc/url", h.GetOIDCAuthorizationURL)
	router.GET("/api/v1/auth/oidc/callback", h.OIDCRedirectCallback)
	router.POST("/api/v1/auth/oidc/result", h.OIDCLoginResult)
	router.POST("/__synthetic/disable", func(c *gin.Context) { enabled.Store(false); c.Status(204) })
	protect := func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		user, tenant, err := users.ValidateToken(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatus(401)
			return
		}
		ctx := context.WithValue(c.Request.Context(), types.UserContextKey, user)
		ctx = context.WithValue(ctx, types.TenantIDContextKey, tenant)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
	router.GET("/api/v1/auth/me", protect, h.GetCurrentUser)
	router.POST("/api/v1/auth/logout", protect, h.Logout)
	router.POST("/api/v1/auth/switch-tenant", protect, h.SwitchTenant)
	router.POST("/api/v1/auth/refresh", h.RefreshToken)
	router.GET("/api/v1/knowledge-bases", protect, func(c *gin.Context) {
		id, _ := types.TenantIDFromContext(c.Request.Context())
		var rows []map[string]interface{}
		if err := db.Table("synthetic_knowledge_bases").Where("tenant_id = ?", id).Find(&rows).Error; err != nil {
			c.Status(500)
			return
		}
		c.JSON(200, gin.H{"success": true, "data": rows})
	})
	router.NoRoute(func(c *gin.Context) { c.JSON(200, gin.H{"success": true, "data": []interface{}{}}) })
	app := httptest.NewServer(router)
	defer app.Close()
	// Isolate server-side correctness from browser cookie/SPA behavior first.
	start := httptest.NewRecorder()
	router.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/url", nil))
	require.Equal(t, 200, start.Code)
	var authorization types.OIDCAuthURLResponse
	require.NoError(t, json.Unmarshal(start.Body.Bytes(), &authorization))
	sidClient := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	approved, err := sidClient.Get(authorization.AuthorizationURL)
	require.NoError(t, err)
	approved.Body.Close()
	callbackURL, err := url.Parse(approved.Header.Get("Location"))
	require.NoError(t, err)
	callback := httptest.NewRequest(http.MethodGet, callbackURL.String(), nil)
	for _, cookie := range start.Result().Cookies() {
		callback.AddCookie(cookie)
	}
	callbackResult := httptest.NewRecorder()
	router.ServeHTTP(callbackResult, callback)
	require.Equal(t, http.StatusFound, callbackResult.Code)
	require.Equal(t, "/#oidc_complete=1", callbackResult.Header().Get("Location"), "synthetic direct business callback must succeed")
	if ordinary {
		resultReq := httptest.NewRequest("POST", "/api/v1/auth/oidc/result", nil)
		resultReq.Header.Set("X-KnowHub-OIDC-Result", "1")
		for _, cookie := range callbackResult.Result().Cookies() {
			resultReq.AddCookie(cookie)
		}
		result := httptest.NewRecorder()
		router.ServeHTTP(result, resultReq)
		require.Equal(t, 200, result.Code)
		var login map[string]interface{}
		require.NoError(t, json.Unmarshal(result.Body.Bytes(), &login))
		_, hasRefresh := login["refresh_token"]
		require.False(t, hasRefresh)
		access, ok := login["token"].(string)
		require.True(t, ok)
		require.NotEmpty(t, access)
		api := func(path, body, token string) *httptest.ResponseRecorder {
			req := httptest.NewRequest("POST", path, strings.NewReader(body))
			if path == "/api/v1/auth/me" {
				req.Method = "GET"
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			return w
		}
		require.Equal(t, 200, api("/api/v1/auth/me", "", access).Code)
		var before types.SourceIDSession
		require.NoError(t, db.First(&before).Error)
		require.Equal(t, 403, api("/api/v1/auth/switch-tenant", `{"tenant_id":999}`, access).Code)
		if role == "multi_space" || role == "cross_space_admin" {
			peer := api("/api/v1/auth/switch-tenant", `{"tenant_id":43}`, access)
			require.Equal(t, 200, peer.Code)
		} else {
			require.Equal(t, 403, api("/api/v1/auth/switch-tenant", `{"tenant_id":43}`, access).Code)
		}
		switched := api("/api/v1/auth/switch-tenant", `{"tenant_id":42}`, access)
		require.Equal(t, 200, switched.Code)
		var next map[string]interface{}
		require.NoError(t, json.Unmarshal(switched.Body.Bytes(), &next))
		_, hasRefresh = next["refresh_token"]
		require.False(t, hasRefresh)
		var after types.SourceIDSession
		require.NoError(t, db.First(&after).Error)
		require.Equal(t, before.ExpiresAt, after.ExpiresAt)
		var refreshCount int64
		require.NoError(t, db.Model(&types.AuthToken{}).Where("token_type = ?", "refresh_token").Count(&refreshCount).Error)
		require.Zero(t, refreshCount)
		require.NotEqual(t, 200, api("/api/v1/auth/refresh", `{"refresh_token":"synthetic-unissued"}`, access).Code)
		require.Equal(t, 200, api("/api/v1/auth/logout", "", access).Code)
		require.Equal(t, 401, api("/api/v1/auth/me", "", access).Code)
		var original types.User
		require.NoError(t, db.First(&original, "id=?", user.ID).Error)
		require.Equal(t, user.TenantID, original.TenantID)
		require.Equal(t, user.IsSystemAdmin, original.IsSystemAdmin)
		require.Equal(t, user.CanAccessAllTenants, original.CanAccessAllTenants)
		var members []types.TenantMember
		require.NoError(t, db.Order("tenant_id").Find(&members).Error)
		require.Equal(t, originalMembers, members)
	}
	if !browser {
		require.EqualValues(t, 1, tokens.Load())
		return
	}
	commandCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "node", "tests/oa-login/sourceid-local-e2e.mjs")
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = filepath.Join(root, "frontend")
	cmd.Env = append(os.Environ(), "SOURCEID_E2E_BACKEND="+app.URL, "SOURCEID_E2E_MOCK="+mock.URL)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "synthetic browser assertions: %s", string(output))
	require.Contains(t, string(output), "SOURCEID_LOCAL_BROWSER_PASS")
	require.EqualValues(t, 2, tokens.Load(), "one code exchange per direct/browser success; denied authorizations exchange none")
	var count int64
	require.NoError(t, db.Model(&types.User{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	var binding types.ExternalIdentity
	require.NoError(t, db.First(&binding).Error)
	require.Equal(t, user.ID, binding.UserID)
	var member types.TenantMember
	require.NoError(t, db.First(&member).Error)
	require.Equal(t, types.TenantRoleContributor, member.Role)
}
