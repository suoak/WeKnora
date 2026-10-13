package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
)

// OIDCProbeMetadata contains public, operator-confirmed metadata only.
// No discovery requests, endpoint guessing, account repositories or business JWTs.
type OIDCProbeMetadata struct {
	Confirmed         bool     `json:"metadata_confirmed"`
	Issuer            string   `json:"issuer"`
	Authorization     string   `json:"authorization_endpoint"`
	Token             string   `json:"token_endpoint"`
	JWKS              string   `json:"jwks_uri"`
	UserInfo          string   `json:"userinfo_endpoint"`
	RedirectURI       string   `json:"redirect_uri"`
	ClientID          string   `json:"client_id"`
	ClientAuth        string   `json:"client_auth_method"`
	UserInfoTransport string   `json:"userinfo_transport"`
	Scopes            []string `json:"scopes"`
	PKCE              bool     `json:"pkce"`
}

type OIDCProbeReport struct {
	Endpoints  map[string]string      `json:"endpoint"`
	Algorithm  string                 `json:"alg,omitempty"`
	Claims     map[string]interface{} `json:"claims,omitempty"`
	UserInfo   map[string]interface{} `json:"userinfo_schema,omitempty"`
	Validation map[string]bool        `json:"validation"`
}

// OIDCProtocolProbe has no repository reference and never creates a user,
// membership, business session or local JWT. Its sole transaction is in memory.
type OIDCProtocolProbe struct {
	mu                        sync.Mutex
	cfg                       config.OIDCAuthConfig
	metadata                  OIDCProbeMetadata
	state, nonce, verifier    string
	started, consumed, closed bool
	deadline                  time.Time
	done                      chan struct{}
	result                    OIDCProbeReport
}

func NewOIDCProtocolProbe(m OIDCProbeMetadata, secret string) (*OIDCProtocolProbe, error) {
	if secret == "" || len(secret) > 4096 {
		return nil, errors.New("probe configuration incomplete")
	}
	if err := ValidateOIDCProbeMetadata(m); err != nil {
		return nil, err
	}
	return newOIDCProtocolProbe(m, secret), nil
}

// ValidateOIDCProbeMetadata runs before reading any credential from stdin.
func ValidateOIDCProbeMetadata(m OIDCProbeMetadata) error {
	if !m.Confirmed || m.ClientID == "" {
		return errors.New("probe configuration incomplete")
	}
	for _, raw := range []string{m.Issuer, m.Authorization, m.Token, m.JWKS, m.UserInfo} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("confirmed HTTPS metadata required")
		}
		if err = validateOIDCEndpoint("probe", raw, true); err != nil {
			return errors.New("probe endpoint rejected")
		}
	}
	u, err := url.Parse(m.RedirectURI)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "/probe/callback" || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("independent approved probe callback required")
	}
	if m.ClientAuth != "client_secret_post" && m.ClientAuth != "client_secret_basic" {
		return errors.New("confirmed client authentication required")
	}
	if m.UserInfoTransport != "bearer_get" && m.UserInfoTransport != "form_post" && m.UserInfoTransport != "multipart_post" {
		return errors.New("confirmed UserInfo transport required")
	}
	openid := false
	for _, scope := range m.Scopes {
		if scope == "offline_access" || strings.ContainsAny(scope, " \t\r\n") {
			return errors.New("probe scope rejected")
		}
		if scope == "openid" {
			openid = true
		}
	}
	if !openid {
		return errors.New("openid scope required")
	}
	return nil
}

func newOIDCProtocolProbe(m OIDCProbeMetadata, secret string) *OIDCProtocolProbe {
	return &OIDCProtocolProbe{metadata: m, cfg: config.OIDCAuthConfig{IssuerURL: m.Issuer, ClientID: m.ClientID, ClientSecret: secret, AuthorizationEndpoint: m.Authorization, TokenEndpoint: m.Token, JwksURI: m.JWKS, UserInfoEndpoint: m.UserInfo, ClientAuthMethod: m.ClientAuth, UserInfoTransport: m.UserInfoTransport}, deadline: time.Now().Add(10 * time.Minute), done: make(chan struct{}), result: OIDCProbeReport{Endpoints: map[string]string{"issuer": m.Issuer, "authorization": m.Authorization, "token": m.Token, "jwks": m.JWKS, "userinfo": m.UserInfo, "callback": m.RedirectURI}, Validation: map[string]bool{"completed": false}}}
}

func (p *OIDCProtocolProbe) Done() <-chan struct{} { return p.done }
func (p *OIDCProtocolProbe) Report() OIDCProbeReport {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result
}
func (p *OIDCProtocolProbe) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		p.cfg.ClientSecret = ""
		p.state = ""
		p.nonce = ""
		p.verifier = ""
		close(p.done)
	}
}

func probeRandom() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func probeJSON(w http.ResponseWriter, status int, report OIDCProbeReport) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(report)
}

func (p *OIDCProtocolProbe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// No logging/recovery framework: panic values must not reach stderr or a dump.
	defer func() {
		if recover() != nil {
			probeJSON(w, 500, OIDCProbeReport{Validation: map[string]bool{"completed": false}})
			p.Close()
		}
	}()
	if r.Method != http.MethodGet {
		probeJSON(w, 405, OIDCProbeReport{Validation: map[string]bool{"method": false}})
		return
	}
	p.mu.Lock()
	if p.closed || time.Now().After(p.deadline) {
		p.mu.Unlock()
		probeJSON(w, 410, OIDCProbeReport{Validation: map[string]bool{"active": false}})
		p.Close()
		return
	}
	if r.URL.Path == "/probe/start" {
		if p.started || r.URL.RawQuery != "" {
			p.mu.Unlock()
			probeJSON(w, 409, OIDCProbeReport{Validation: map[string]bool{"transaction": false}})
			return
		}
		state, e1 := probeRandom()
		nonce, e2 := probeRandom()
		verifier, e3 := probeRandom()
		if e1 != nil || e2 != nil || e3 != nil {
			p.mu.Unlock()
			probeJSON(w, 500, OIDCProbeReport{Validation: map[string]bool{"transaction": false}})
			p.Close()
			return
		}
		p.state = state
		p.nonce = nonce
		p.verifier = verifier
		p.started = true
		m := p.metadata
		p.mu.Unlock()
		u, _ := url.Parse(m.Authorization)
		q := u.Query()
		q.Set("response_type", "code")
		q.Set("client_id", m.ClientID)
		q.Set("redirect_uri", m.RedirectURI)
		q.Set("scope", strings.Join(m.Scopes, " "))
		q.Set("state", state)
		q.Set("nonce", nonce)
		if m.PKCE {
			sum := sha256.Sum256([]byte(verifier))
			q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
			q.Set("code_challenge_method", "S256")
		}
		u.RawQuery = q.Encode()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// Do not include the authorization URL in a redirect response body.
		w.Header().Set("Location", u.String())
		w.WriteHeader(http.StatusFound)
		return
	}
	if r.URL.Path != "/probe/callback" {
		p.mu.Unlock()
		probeJSON(w, 404, OIDCProbeReport{Validation: map[string]bool{"route": false}})
		return
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || !p.started || p.consumed || len(q["state"]) != 1 || q.Get("state") != p.state {
		p.mu.Unlock()
		probeJSON(w, 400, OIDCProbeReport{Validation: map[string]bool{"state": false}})
		return
	}
	p.consumed = true
	cfg := p.cfg
	nonce := p.nonce
	verifier := p.verifier
	pkce := p.metadata.PKCE
	redirect := p.metadata.RedirectURI
	p.state = ""
	p.nonce = ""
	p.verifier = ""
	result := OIDCProbeReport{Endpoints: p.result.Endpoints, Validation: map[string]bool{"state": true, "completed": false}}
	p.mu.Unlock()
	defer p.Close()
	if len(q["code"]) != 1 || q.Get("code") == "" || len(q.Get("code")) > 8192 || len(q["error"]) != 0 {
		p.finish(w, 400, result)
		return
	}
	ctx := r.Context()
	if pkce {
		ctx = context.WithValue(ctx, oidcVerifierKey, verifier)
	}
	client := newOIDCHTTPClient()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	svc := &userService{protocolClient: client} // Intentionally no user/token repositories or config/container.
	tokens, err := svc.exchangeOIDCCode(ctx, &cfg, q.Get("code"), redirect)
	result.Validation["token_exchange"] = err == nil
	if err != nil {
		p.finish(w, 502, result)
		return
	}
	// The existing decoder deliberately has no refresh_token field; it is ignored.
	defer func() { tokens.AccessToken = ""; tokens.IDToken = ""; cfg.ClientSecret = "" }()
	if tokens.IDToken == "" || tokens.AccessToken == "" {
		p.finish(w, 502, result)
		return
	}
	claims, err := svc.verifyOIDCIDToken(ctx, &cfg, tokens.IDToken)
	result.Validation["id_token"] = err == nil
	if err != nil {
		p.finish(w, 400, result)
		return
	}
	returnedNonce, ok := claims["nonce"].(string)
	result.Validation["nonce"] = ok && returnedNonce == nonce && nonce != ""
	if !result.Validation["nonce"] {
		p.finish(w, 400, result)
		return
	}
	for _, check := range []string{"signature", "issuer", "audience", "authorized_party", "expiry", "issued_at", "subject"} {
		result.Validation[check] = true
	}
	header, err := base64.RawURLEncoding.DecodeString(strings.Split(tokens.IDToken, ".")[0])
	if err != nil {
		p.finish(w, 400, result)
		return
	}
	var h struct {
		Algorithm string `json:"alg"`
	}
	if json.Unmarshal(header, &h) != nil {
		p.finish(w, 400, result)
		return
	}
	result.Algorithm = h.Algorithm
	budget := 512
	result.Claims = probeSchema(claims, 0, &budget)
	info, err := svc.fetchOIDCUserInfoWithTransport(ctx, cfg.UserInfoEndpoint, tokens.AccessToken, cfg.UserInfoTransport)
	result.Validation["userinfo"] = err == nil
	if err != nil {
		p.finish(w, 502, result)
		return
	}
	// Preserve schema-only protocol evidence even when subject validation fails.
	// No identity values are exposed and the transaction still fails closed.
	budget = 512
	result.UserInfo = probeSchema(info, 0, &budget)
	sub, ok := info["sub"].(string)
	result.Validation["userinfo_subject"] = ok && sub != "" && sub == claims["sub"]
	if !result.Validation["userinfo_subject"] {
		p.finish(w, 400, result)
		return
	}
	result.Validation["completed"] = true
	p.finish(w, 200, result)
}

func (p *OIDCProtocolProbe) finish(w http.ResponseWriter, status int, result OIDCProbeReport) {
	p.mu.Lock()
	p.result = result
	p.mu.Unlock()
	probeJSON(w, status, result)
}

var probeFieldName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,63}$`)

func probeSchema(object map[string]interface{}, depth int, budget *int) map[string]interface{} {
	out := map[string]interface{}{}
	if depth >= 6 {
		return map[string]interface{}{"_limited": true}
	}
	for key, value := range object {
		if *budget <= 0 {
			out["_limited"] = true
			break
		}
		if !probeFieldName.MatchString(key) {
			out["_omitted_field_names"] = true
			continue
		}
		*budget--
		switch v := value.(type) {
		case map[string]interface{}:
			out[key] = map[string]interface{}{"type": "object", "fields": probeSchema(v, depth+1, budget)}
		case []interface{}:
			out[key] = map[string]interface{}{"type": "array"}
		case string:
			out[key] = "string"
		case bool:
			out[key] = "boolean"
		case float64:
			out[key] = "number"
		case nil:
			out[key] = "null"
		default:
			out[key] = "unknown"
		}
	}
	return out
}

func (s *userService) oidcProtocolHTTPClient() *http.Client {
	if s.protocolClient != nil {
		return s.protocolClient
	}
	return newOIDCHTTPClient()
}
