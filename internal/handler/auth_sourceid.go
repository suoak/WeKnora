package handler

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/Tencent/WeKnora/internal/sourceid"
	"github.com/gin-gonic/gin"
)

type sourceIDAuthService interface {
	PrepareOIDCCallback(context.Context, string) (context.Context, error)
	SourceIDBackchannelLogout(context.Context, string, string) error
	SourceIDSwitchContext(context.Context, string) (context.Context, error)
	SaveOIDCLoginResult(context.Context, string) (string, error)
	ConsumeOIDCLoginResult(context.Context, string) (string, error)
}

// This header belongs to a mutually authenticated ingress contract, NOT a
// presumed native SourceID header. Ingress must strip incoming copies and set
// it only after verifying the SourceID sender. Direct peer CIDRs are required.
func trustedSourceIDPeer(r *http.Request, cidrs, secret string) bool {
	if len(secret) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-KnowHub-SourceID-Secret")), []byte(secret)) != 1 {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil || strings.TrimSpace(cidrs) == "" {
		return false
	}
	allowed := false
	for _, value := range strings.Split(cidrs, ",") {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err != nil {
			return false
		}
		if network.Contains(ip) {
			allowed = true
		}
	}
	return allowed
}

func (h *AuthHandler) SourceIDBackchannelLogout(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.configInfo == nil || h.configInfo.OIDCAuth == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	cfg := h.configInfo.OIDCAuth
	if !cfg.Enable || !cfg.SourceIDEnable || !cfg.SourceIDLogoutEnable || !cfg.SourceIDContractConfirmed || cfg.IssuerURL == "" {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if !trustedSourceIDPeer(c.Request, cfg.SourceIDLogoutCIDRs, cfg.SourceIDLogoutSecret) {
		c.Status(http.StatusForbidden)
		return
	}
	contentType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || contentType != "application/x-www-form-urlencoded" {
		c.Status(http.StatusUnsupportedMediaType)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	if err := c.Request.ParseForm(); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	values := c.Request.PostForm["logoutRequest"]
	if len(values) != 1 || len(c.Request.URL.Query()) != 0 {
		c.Status(http.StatusBadRequest)
		return
	}
	request, err := sourceid.ParseLogout(values[0], cfg.SourceIDLogoutEncoding)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	service, ok := h.userService.(sourceIDAuthService)
	if !ok {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if err := service.SourceIDBackchannelLogout(c.Request.Context(), cfg.IssuerURL, request.SessionIndex); err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	// Empty HTTP 200 is the local contract; production requires confirmation.
	c.Status(http.StatusOK)
}

// The opaque cookie prevents local JWTs entering URLs, history or referrers.
func (h *AuthHandler) OIDCLoginResult(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	if c.GetHeader("X-KnowHub-OIDC-Result") != "1" {
		c.Status(http.StatusForbidden)
		return
	}
	// Do not depend on a permissive deployment's CORS configuration for CSRF.
	if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		c.Status(http.StatusForbidden)
		return
	}
	if origin := c.GetHeader("Origin"); origin != "" {
		expected := oidcCallbackURL(c)
		configuredCallback := false
		if h.configInfo != nil && h.configInfo.OIDCAuth != nil && h.configInfo.OIDCAuth.RedirectURI != "" {
			expected = h.configInfo.OIDCAuth.RedirectURI
			configuredCallback = true
		}
		u, err := url.Parse(expected)
		// Same-origin Fetch Metadata is browser-controlled and survives a dev
		// proxy rewriting Host. A configured callback must always match exactly.
		if err != nil || (origin != u.Scheme+"://"+u.Host && (configuredCallback || c.GetHeader("Sec-Fetch-Site") != "same-origin")) {
			c.Status(http.StatusForbidden)
			return
		}
	}
	key, err := c.Cookie("knowhub_oidc_result")
	c.SetCookie("knowhub_oidc_result", "", -1, "/api/v1/auth/oidc", "", c.Request.TLS != nil, true)
	service, ok := h.userService.(sourceIDAuthService)
	if err != nil || !ok {
		c.Status(http.StatusUnauthorized)
		return
	}
	encoded, err := service.ConsumeOIDCLoginResult(c.Request.Context(), key)
	if err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	c.Data(http.StatusOK, "application/json", payload)
}

// Only known, non-sensitive categories may reach the browser. SQL/provider
// bodies and arbitrary errors never become user-facing descriptions.
func oidcPublicError(err error) string {
	if err == nil {
		return "login_failed"
	}
	switch err.Error() {
	case "sourceid identity contract pending":
		return "identity_contract_pending"
	case "sourceid enrollment requires review":
		return "enrollment_required"
	case "sourceid identity conflict":
		return "identity_conflict"
	case "sourceid employee disabled", "sourceid account unavailable":
		return "account_disabled"
	case "sourceid employee or session missing":
		return "identity_fields_missing"
	case "OIDC ID Token nonce mismatch":
		return "invalid_identity"
	default:
		return "login_failed"
	}
}
