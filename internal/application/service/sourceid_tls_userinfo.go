package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/golang-jwt/jwt/v5"
)

type sidTLSAuthorizationKey struct{}

// This context capability can only be created after durable state consumption.
// Neither it nor the receipt is exported, serialized, or supplied by a browser.
type sidTLSAuthorization struct {
	service          *userService
	binding          [32]byte
	nonce            string
	started, expires time.Time
	used             atomic.Bool
}

type sidTLSReceipt struct {
	authorization      *sidTLSAuthorization
	idHash, accessHash [32]byte
	codeHash           string
	used               atomic.Bool
}

func sidTLSUserInfoMode(c *config.OIDCAuthConfig) bool {
	return c != nil && c.EffectiveSourceIDValidationStrategy() == config.SourceIDTLSUserInfoValidation
}

func sidTLSConfigurationBinding(c *config.OIDCAuthConfig) [32]byte {
	secret := sha256.Sum256([]byte(c.ClientSecret))
	encoded, _ := json.Marshal([]interface{}{
		c.EffectiveSourceIDValidationStrategy(), c.IssuerURL, c.ClientID,
		hex.EncodeToString(
			secret[:],
		), c.RedirectURI, c.AuthorizationEndpoint, c.TokenEndpoint, c.UserInfoEndpoint,
		c.ClientAuthMethod, c.SourceIDBasicEncoding, c.UserInfoTransport, c.SourceIDClaimsSource,
		c.SourceIDTLSUserInfoApprovalRef, c.SourceIDTLSUserInfoExpiresAt,
		c.SourceIDTLSUserInfoLifecycle, c.SourceIDTLSUserInfoTestInstance,
		c.SourceIDForceReauthPromptLogin,
		c.PKCEEnable,
		c.UserInfoMapping,
		c.SourceIDEmployeeClaim,
		c.SourceIDSessionClaim,

		c.SourceIDAccountLinkMode, c.SourceIDAccountScope, c.SourceIDAdmissionMode, c.SourceIDAdmissionPolicyRef,
	})
	return sha256.Sum256(encoded)
}

func validateSIDTLSResolved(c *config.OIDCAuthConfig) error {
	if err := c.ValidateSIDTLSUserInfoPolicy(); err != nil {
		return err
	}
	if c.AuthorizationEndpoint != config.SourceIDPinnedIssuer+"/oidc/authorize" ||
		c.TokenEndpoint != config.SourceIDPinnedIssuer+
			"/oidc/accessToken" || c.UserInfoEndpoint != config.SourceIDPinnedIssuer+
		"/oidc/profile" {
		return errors.New("SID TLS resolved endpoints invalid")
	}
	return nil
}

func (s *userService) prepareSIDTLSAuthorization(
	ctx context.Context,
	c *config.OIDCAuthConfig,
	transaction *types.OIDCTransaction,
) (context.Context, error) {
	if err := validateSIDTLSResolved(c); err != nil {
		return ctx, err
	}
	nonce, _ := ctx.Value(oidcNonceKey).(string)
	metadata, _ := ctx.Value(sourceIDReauthRequestKey{}).(sourceIDReauthRequest)
	binding := sidTLSConfigurationBinding(c)
	now := time.Now()
	if nonce == "" || transaction.Nonce != nonce || metadata.Version != 1 ||
		metadata.TLSBinding != hex.EncodeToString(binding[:]) ||
		metadata.Started.IsZero() ||
		metadata.Started.After(now) ||
		!now.Before(transaction.ExpiresAt) {
		return ctx, errors.New("SID TLS consumed transaction metadata invalid")
	}
	expires := now.Add(2 * time.Minute)
	if transaction.ExpiresAt.Before(expires) {
		expires = transaction.ExpiresAt
	}
	tx := &sidTLSAuthorization{
		service: s,
		binding: binding,
		nonce:   nonce,
		started: metadata.Started,
		expires: expires,
	}
	return context.WithValue(ctx, sidTLSAuthorizationKey{}, tx), nil
}

type sidTLSResponseGuard struct {
	base            http.RoundTripper
	endpoint, phase string
}

func (g *sidTLSResponseGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || req.URL.String() != g.endpoint || req.URL.Scheme != "https" ||
		req.URL.Host != "sid.ruijie.com.cn" ||
		req.URL.User != nil ||
		(req.Host != "" && req.Host != "sid.ruijie.com.cn") {
		return nil, errors.New("SID TLS request endpoint mismatch")
	}
	// Authentication clients do not own a shared connection pool.
	req = req.Clone(req.Context())
	req.Close = true
	resp, err := g.base.RoundTrip(req)
	if err != nil {
		sidTLSAudit(req.Context(), g.phase, nil, false)
		return nil, errors.New("SID trusted TLS request failed")
	}
	reject := func() (*http.Response, error) {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		sidTLSAudit(req.Context(), g.phase, resp, false)
		return nil, errors.New("SID trusted TLS response rejected")
	}
	if resp == nil || resp.Body == nil || resp.StatusCode != http.StatusOK || resp.Request == nil ||
		resp.Request.URL.String() != g.endpoint ||
		resp.TLS == nil ||
		len(resp.TLS.VerifiedChains) == 0 ||
		len(resp.TLS.PeerCertificates) == 0 ||
		resp.TLS.Version < tls.VersionTLS12 ||
		resp.TLS.PeerCertificates[0].VerifyHostname("sid.ruijie.com.cn") != nil {
		return reject()
	}
	contentType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return reject()
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	_ = resp.Body.Close()
	if err != nil || len(body) > 64<<10 {
		return reject()
	}
	if _, err = sidPinnedJSONObject(body); err != nil {
		return reject()
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	sidTLSAudit(req.Context(), g.phase, resp, true)
	return resp, nil
}

func sidTLSAudit(ctx context.Context, phase string, resp *http.Response, pass bool) {
	entry, ok := ctx.Value(sourceIDDiagnosticKey{}).(sourceIDDiagnosticContext)
	if !ok {
		return
	}
	version := uint16(0)
	if resp != nil && resp.TLS != nil {
		version = resp.TLS.Version
	}
	data, _ := json.Marshal(struct {
		Event      string `json:"event"`
		Trace      string `json:"trace"`
		Strategy   string `json:"strategy"`
		Phase      string `json:"phase"`
		Pass       bool   `json:"pass"`
		TLSVersion uint16 `json:"tls_version"`
	}{"sourceid_tls_origin", entry.ID, config.SourceIDTLSUserInfoValidation, phase, pass, version})
	sourceIDEmitTokenDiagnostic(ctx, data)
}

func (s *userService) sidTLSHTTPClient(endpoint, phase string) (*http.Client, error) {
	base := s.oidcProtocolHTTPClient()
	transport := base.Transport
	if wrapped, ok := transport.(*secutils.SSRFValidatingRoundTripper); ok {
		transport = wrapped.Base
	}
	t, ok := transport.(*http.Transport)
	//nolint:staticcheck // Reject deprecated TLS overrides; they also bypass server verification.
	if !ok || t.DialTLS != nil || (s.protocolClient != nil && t.DialTLSContext != nil) {
		return nil, errors.New("SID TLS transport not trusted")
	}
	if t.TLSClientConfig != nil &&
		(t.TLSClientConfig.InsecureSkipVerify ||
			(t.TLSClientConfig.ServerName != "" &&
				t.TLSClientConfig.ServerName != "sid.ruijie.com.cn") ||
			(t.TLSClientConfig.MinVersion != 0 &&
				t.TLSClientConfig.MinVersion < tls.VersionTLS12) ||
			(t.TLSClientConfig.MaxVersion != 0 &&
				t.TLSClientConfig.MaxVersion < tls.VersionTLS12)) {
		return nil, errors.New("SID TLS verification cannot be disabled")
	}
	// Keep the production SSRF-safe DNS/TLS fallback and connection policy.
	// Never unwrap it for requests, or replace its verified TLS dialer.
	copyClient := *base
	copyClient.Jar = nil
	copyClient.Timeout = 30 * time.Second
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	copyClient.Transport = &sidTLSResponseGuard{base: base.Transport, endpoint: endpoint, phase: phase}
	return &copyClient, nil
}

func (s *userService) exchangeSIDTLSCode(
	ctx context.Context,
	c *config.OIDCAuthConfig,
	code, redirect string,
) (*oidcTokenResponse, error) {
	if err := validateSIDTLSResolved(c); err != nil {
		return nil, err
	}
	tx, _ := ctx.Value(sidTLSAuthorizationKey{}).(*sidTLSAuthorization)
	nonce, _ := ctx.Value(oidcNonceKey).(string)
	if tx == nil || tx.service != s || tx.binding != sidTLSConfigurationBinding(c) || nonce == "" ||
		tx.nonce != nonce ||
		!time.Now().Before(tx.expires) ||
		redirect != c.RedirectURI ||
		strings.TrimSpace(code) == "" ||
		strings.TrimSpace(code) != code ||
		len(code) > 8192 ||
		!tx.used.CompareAndSwap(false, true) {
		return nil, errors.New("SID TLS fresh consumed authorization required")
	}
	store, err := s.sourceIDStore()
	if err != nil {
		return nil, err
	}
	codeDigest := sha256.Sum256([]byte(code))
	key := apprepo.SourceIDHash(
		"sid-tls-code:" + c.IssuerURL + ":" + c.ClientID + ":" + hex.EncodeToString(codeDigest[:]),
	)
	if err = store.SaveOIDCTransaction(
		ctx,
		&types.OIDCTransaction{
			Key:      key,
			Consumed: true,
			ExpiresAt: time.Now().
				Add(
					24 * time.Hour,
				),
		},
	); err != nil {
		return nil, errors.New("SID authorization code reserved or storage unavailable")
	}
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirect},
		"client_id":    {c.ClientID},
	}
	if verifier, _ := ctx.Value(oidcVerifierKey).(string); verifier != "" {
		form.Set("code_verifier", verifier)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.TokenEndpoint,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, errors.New("SID Token request invalid")
	}
	encoding, err := c.EffectiveSourceIDBasicEncoding()
	if err != nil {
		return nil, err
	}
	if encoding == "raw" {
		req.SetBasicAuth(c.ClientID, c.ClientSecret)
	} else {
		req.SetBasicAuth(url.QueryEscape(c.ClientID), url.QueryEscape(c.ClientSecret))
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	client, err := s.sidTLSHTTPClient(c.TokenEndpoint, "token")
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("SID Token trusted exchange failed")
	}
	defer func(cleanup func() error) { _ = cleanup() }(resp.Body.Close)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.New("SID Token response unreadable")
	}
	data, err := sidPinnedJSONObject(body)
	if err != nil {
		return nil, err
	}
	if _, present := data["error"]; present {
		return nil, errors.New("SID Token response contains error")
	}
	access, aok := data["access_token"].(string)
	id, iok := data["id_token"].(string)
	kind, kok := data["token_type"].(string)
	if !aok || !iok || !kok || access == "" || id == "" || len(access) > 16<<10 || len(id) > 16<<10 ||
		strings.TrimSpace(access) != access ||
		strings.TrimSpace(id) != id ||
		!strings.EqualFold(kind, "Bearer") {
		return nil, errors.New("SID complete Bearer Token response required")
	}
	proof := &sidTLSReceipt{
		authorization: tx,
		idHash:        sha256.Sum256([]byte(id)),
		accessHash:    sha256.Sum256([]byte(access)),
		codeHash:      base64.RawURLEncoding.EncodeToString(codeDigest[:16]),
	}
	return &oidcTokenResponse{AccessToken: access, IDToken: id, TokenType: kind, sidTLSProof: proof}, nil
}

func (s *userService) checkSIDTLSReceipt(
	ctx context.Context,
	c *config.OIDCAuthConfig,
	r *oidcTokenResponse,
	consume bool,
) (*sidTLSAuthorization, error) {
	if err := validateSIDTLSResolved(c); err != nil {
		return nil, err
	}
	tx, _ := ctx.Value(sidTLSAuthorizationKey{}).(*sidTLSAuthorization)
	if r == nil || tx == nil || tx.service != s || r.sidTLSProof == nil ||
		r.sidTLSProof.authorization != tx ||
		tx.binding != sidTLSConfigurationBinding(c) ||
		!tx.used.Load() ||
		!time.Now().Before(tx.expires) ||
		r.sidTLSProof.idHash != sha256.Sum256([]byte(r.IDToken)) ||
		r.sidTLSProof.accessHash != sha256.Sum256([]byte(r.AccessToken)) {
		return nil, errors.New("SID Token lacks current trusted exchange provenance")
	}
	nonce, _ := ctx.Value(oidcNonceKey).(string)
	if nonce == "" || nonce != tx.nonce {
		return nil, errors.New("SID Token transaction nonce mismatch")
	}
	if consume && !r.sidTLSProof.used.CompareAndSwap(false, true) {
		return nil, errors.New("SID Token provenance already consumed")
	}
	return tx, nil
}

func validateSIDTLSClaims(
	ctx context.Context,
	c *config.OIDCAuthConfig,
	r *oidcTokenResponse,
	tx *sidTLSAuthorization,
) (map[string]interface{}, error) {
	parts := strings.Split(r.IDToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("SID signed JWT structure required")
	}
	headerBytes, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("SID JWT header encoding invalid")
	}
	header, err := sidPinnedJSONObject(headerBytes)
	if err != nil || header["alg"] != "RS256" {
		return nil, errors.New("SID RS256 JWT header required")
	}
	for _, k := range []string{"jku", "jwk", "x5u", "x5c", "crit", "b64"} {
		if _, ok := header[k]; ok {
			return nil, errors.New("SID JWT external trust or extension rejected")
		}
	}
	if kid, present := header["kid"]; present {
		v, ok := kid.(string)
		if !ok || len(v) > 128 {
			return nil, errors.New("SID JWT kid invalid")
		}
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || len(signature) < 256 || len(signature) > 512 {
		return nil, errors.New("SID RSA signature structure invalid")
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("SID JWT claims encoding invalid")
	}
	data, err := sidPinnedJSONObject(payload)
	if err != nil {
		return nil, err
	}
	for _, k := range []string{"exp", "iat", "nbf"} {
		v, exists := data[k]
		if !exists {
			if k != "nbf" {
				return nil, errors.New("SID required NumericDate missing")
			}
			continue
		}
		n, ok := v.(json.Number)
		if !ok {
			return nil, errors.New("SID NumericDate type invalid")
		}
		seconds, e := n.Int64()
		if e != nil || seconds < 0 {
			return nil, errors.New("SID NumericDate invalid")
		}
	}
	claims := jwt.MapClaims(data)
	if err = jwt.NewValidator(
		jwt.WithIssuer(
			c.IssuerURL,
		),
		jwt.WithAudience(
			c.ClientID,
		),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(
			oidcIDTokenLeeway,
		),
	).
		Validate(
			claims,
		); err != nil {
		return nil, errors.New("SID TLS ID Token claims rejected")
	}
	iat, err := claims.GetIssuedAt()
	if err != nil || iat == nil || iat.Before(tx.started.Add(-oidcIDTokenLeeway)) {
		return nil, errors.New("SID ID Token issued before current authorization")
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil || !exp.After(iat.Time) {
		return nil, errors.New("SID ID Token expiry invalid")
	}
	sub, ok := data["sub"].(string)
	if !ok || strings.TrimSpace(sub) == "" || sub != strings.TrimSpace(sub) || len(sub) > 255 {
		return nil, errors.New("SID ID Token subject invalid")
	}
	aud, err := claims.GetAudience()
	azp, present := data["azp"]
	if err != nil || (present && azp != c.ClientID) || (len(aud) > 1 && azp != c.ClientID) {
		return nil, errors.New("SID ID Token authorized party invalid")
	}
	nonce, ok := data["nonce"].(string)
	if !ok || subtle.ConstantTimeCompare([]byte(nonce), []byte(tx.nonce)) != 1 {
		return nil, errors.New("OIDC ID Token nonce mismatch")
	}
	for k, want := range map[string]string{"at_hash": sidTLSHalfHash(r.AccessToken), "c_hash": r.sidTLSProof.codeHash} {
		if got, exists := data[k]; exists {
			v, ok := got.(string)
			if !ok || subtle.ConstantTimeCompare([]byte(v), []byte(want)) != 1 {
				return nil, errors.New("SID Token hash mismatch")
			}
		}
	}
	if err = checkSourceIDReauthentication(ctx, c, data); err != nil {
		return nil, err
	}
	return data, nil
}

func sidTLSHalfHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

func sidTLSSecurityClaim(k string) bool {
	switch k {
	case "iss", "sub", "aud", "azp", "exp", "iat", "nbf", "nonce", "at_hash", "c_hash", "auth_time":
		return true
	}
	return false
}

func (s *userService) resolveSIDTLSUserInfo(
	ctx context.Context,
	c *config.OIDCAuthConfig,
	r *oidcTokenResponse,
) (*types.OIDCUserInfo, error) {
	tx, err := s.checkSIDTLSReceipt(ctx, c, r, true)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	idClaims, err := validateSIDTLSClaims(ctx, c, r, tx)
	SourceIDDiagnosticStage(ctx, "id_token", started, err)
	if err != nil {
		return nil, err
	}
	client, err := s.sidTLSHTTPClient(c.UserInfoEndpoint, "userinfo")
	if err != nil {
		return nil, err
	}
	started = time.Now()
	profile, err := s.fetchOIDCUserInfoWithClient(
		ctx,
		c.UserInfoEndpoint,
		r.AccessToken,
		c.UserInfoTransport,
		client,
	)
	if err == nil {
		subject, ok := profile["sub"].(string)
		if !ok || subject != idClaims["sub"] {
			err = errors.New("OIDC UserInfo subject mismatch")
		}
	}
	SourceIDDiagnosticStage(ctx, "userinfo", started, err)
	if err != nil {
		return nil, errors.New("SID UserInfo response failed or subject mismatch")
	}
	if _, err = s.checkSIDTLSReceipt(ctx, c, r, false); err != nil {
		return nil, err
	}
	if _, err = validateSIDTLSClaims(ctx, c, r, tx); err != nil {
		return nil, err
	}
	// Security claims remain from the authenticated Token response. Employee
	// attributes come exclusively from subject-bound UserInfo, never fallback.
	claims := map[string]interface{}{}
	for k, v := range idClaims {
		if sidTLSSecurityClaim(k) {
			claims[k] = v
		}
	}
	for k, v := range profile {
		if !sidTLSSecurityClaim(k) {
			claims[k] = v
		}
	}
	info := &types.OIDCUserInfo{Claims: claims, Subject: idClaims["sub"].(string)}
	info.Username = extractClaimAsString(claims, c.UserInfoMapping.Username)
	info.Email = extractClaimAsString(claims, c.UserInfoMapping.Email)
	if info.Username == "" {
		info.Username = extractClaimAsString(claims, "preferred_username")
	}
	if info.Username == "" {
		info.Username = extractClaimAsString(claims, "name")
	}
	if info.Username == "" && info.Email != "" {
		info.Username = strings.Split(info.Email, "@")[0]
	}
	return info, nil
}
