package service

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
)

const (
	sidPinnedMaxKeys  = 8
	sidPinnedMaxBytes = 32 << 10
)

type sidPinnedJWK struct {
	Kty    string   `json:"kty"`
	Kid    string   `json:"kid,omitempty"`
	Alg    string   `json:"alg,omitempty"`
	Use    string   `json:"use,omitempty"`
	N      string   `json:"n"`
	E      string   `json:"e"`
	KeyOps []string `json:"key_ops,omitempty"`
}

type sidPinnedKeyDocument struct {
	Version     int            `json:"version"`
	Issuer      string         `json:"issuer"`
	ClientID    string         `json:"client_id"`
	ApprovalRef string         `json:"approval_ref"`
	NotBefore   time.Time      `json:"not_before"`
	ExpiresAt   time.Time      `json:"expires_at"`
	Revoked     bool           `json:"revoked"`
	Keys        []sidPinnedJWK `json:"keys"`
}

type sidPinnedCandidate struct {
	key         *rsa.PublicKey
	fingerprint string
	kids        map[string]bool
}
type sidPinnedSet struct {
	candidates []sidPinnedCandidate
	digest     string
	expires    time.Time
}

func sidPinnedMode(c *config.OIDCAuthConfig) bool {
	return c != nil && c.EffectiveSourceIDValidationStrategy() == config.SourceIDPinnedValidation
}

func validateSIDPinnedEndpoints(c *config.OIDCAuthConfig) error {
	for _, endpoint := range []string{c.AuthorizationEndpoint, c.TokenEndpoint, c.UserInfoEndpoint} {
		u, e := url.Parse(endpoint)
		if e != nil || u.Scheme != "https" || u.Host != "sid.ruijie.com.cn" || u.User != nil ||
			u.RawQuery != "" ||
			u.Fragment != "" {
			return errors.New("SID pinned mode requires trusted HTTPS SID endpoints")
		}
	}
	if c.AuthorizationEndpoint != config.SourceIDPinnedIssuer+"/oidc/authorize" ||
		c.TokenEndpoint != config.SourceIDPinnedIssuer+"/oidc/accessToken" ||
		c.UserInfoEndpoint != config.SourceIDPinnedIssuer+"/oidc/profile" {
		return errors.New("SID pinned endpoints differ from Discovery contract")
	}
	return nil
}

// RFC 7638 thumbprint over canonical public RSA material; formatting and JWK
// metadata do not create additional distinct trusted keys.
func sidPinnedFingerprint(key *rsa.PublicKey) string {
	n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
	raw := `{"e":"AQAB","kty":"RSA","n":"` + n + `"}`
	h := sha256.Sum256([]byte(raw))
	return "SHA256:" + base64.RawURLEncoding.EncodeToString(h[:])
}

// Read on every authentication. No previous keys, JWKS fetches, or successful
// verifications are retained as trust after removal, revocation, or expiry.
func loadSIDPinnedKeys(c *config.OIDCAuthConfig, now time.Time) (*sidPinnedSet, error) {
	if e := c.ValidateSourceIDValidationStrategy(); e != nil {
		return nil, e
	}
	if !sidPinnedMode(c) {
		return nil, errors.New("SID pinned key mode required")
	}
	deadline, e := time.Parse(time.RFC3339, c.SourceIDPinnedKeysExpiresAt)
	if e != nil || !now.Before(deadline) {
		return nil, errors.New("SID pinned key approval expired")
	}
	approved := map[string]bool{}
	values := strings.Split(c.SourceIDPinnedKeyFingerprints, ",")
	if len(values) > sidPinnedMaxKeys {
		return nil, errors.New("SID fingerprint approval limit exceeded")
	}
	for _, v := range values {
		v = strings.TrimSpace(v)
		raw, e := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(v, "SHA256:"))
		if !strings.HasPrefix(v, "SHA256:") || e != nil || len(raw) != 32 || approved[v] {
			return nil, errors.New("SID fingerprint approval invalid")
		}
		approved[v] = true
	}
	before, e := os.Lstat(c.SourceIDPinnedKeysFile)
	if e != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0o022 != 0 ||
		before.Size() > sidPinnedMaxBytes {
		return nil, errors.New("SID runtime key file unavailable or unsafe")
	}
	f, e := os.Open(c.SourceIDPinnedKeysFile)
	if e != nil {
		return nil, errors.New("SID runtime key file unavailable")
	}
	defer func(cleanup func() error) { _ = cleanup() }(f.Close)
	after, e := f.Stat()
	if e != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() {
		return nil, errors.New("SID runtime key file changed during open")
	}
	raw, e := io.ReadAll(io.LimitReader(f, sidPinnedMaxBytes+1))
	if e != nil || len(raw) > sidPinnedMaxBytes {
		return nil, errors.New("SID runtime key file too large")
	}
	if _, e = sidPinnedJSONObject(raw); e != nil {
		return nil, e
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var doc sidPinnedKeyDocument
	if e = decoder.Decode(&doc); e != nil {
		return nil, errors.New("SID runtime key schema invalid")
	}
	if doc.Version != 1 || doc.Issuer != c.IssuerURL || doc.ClientID != c.ClientID ||
		doc.ApprovalRef != c.SourceIDPinnedKeysApprovalRef ||
		doc.Revoked {
		return nil, errors.New("SID runtime key approval mismatch or revoked")
	}
	if doc.NotBefore.IsZero() || doc.ExpiresAt.IsZero() || doc.NotBefore.After(now) ||
		!now.Before(doc.ExpiresAt) ||
		doc.ExpiresAt.After(deadline) ||
		!doc.ExpiresAt.After(doc.NotBefore) ||
		doc.ExpiresAt.Sub(doc.NotBefore) > 30*24*time.Hour {
		return nil, errors.New("SID runtime key validity rejected")
	}
	if len(doc.Keys) == 0 || len(doc.Keys) > sidPinnedMaxKeys {
		return nil, errors.New("SID runtime key candidate limit rejected")
	}
	unique := map[string]*sidPinnedCandidate{}
	kidOwners := map[string]string{}
	for _, j := range doc.Keys {
		if j.Kty != "RSA" || (j.Alg != "" && j.Alg != "RS256") || (j.Use != "" && j.Use != "sig") ||
			len(j.N) > 700 ||
			len(j.E) > 8 ||
			len(j.Kid) > 128 {
			return nil, errors.New("SID public key policy rejected")
		}
		if j.KeyOps != nil && (len(j.KeyOps) != 1 || j.KeyOps[0] != "verify") {
			return nil, errors.New("SID public key operations rejected")
		}
		n, e := base64.RawURLEncoding.Strict().DecodeString(j.N)
		if e != nil || len(n) < 256 || len(n) > 513 {
			return nil, errors.New("SID public modulus invalid")
		}
		exponent, e := base64.RawURLEncoding.Strict().DecodeString(j.E)
		if e != nil || len(exponent) > 4 {
			return nil, errors.New("SID public exponent invalid")
		}
		key, e := (oidcJWK{Kty: j.Kty, N: j.N, E: j.E}).rsaPublicKey()
		if e != nil || key.E != 65537 || key.N.BitLen() < 2048 || key.N.BitLen() > 4096 ||
			key.N.Bit(0) != 1 {
			return nil, errors.New("SID RSA key strength rejected")
		}
		fp := sidPinnedFingerprint(key)
		if !approved[fp] {
			return nil, errors.New("SID public key not approved")
		}
		if j.Kid != "" {
			if owner, ok := kidOwners[j.Kid]; ok && owner != fp {
				return nil, errors.New("SID duplicate kid is ambiguous")
			}
			kidOwners[j.Kid] = fp
		}
		candidate := unique[fp]
		if candidate == nil {
			candidate = &sidPinnedCandidate{key: key, fingerprint: fp, kids: map[string]bool{}}
			unique[fp] = candidate
		}
		if j.Kid != "" {
			candidate.kids[j.Kid] = true
		}
	}
	fps := make([]string, 0, len(unique))
	for fp := range unique {
		fps = append(fps, fp)
	}
	sort.Strings(fps)
	digest := sha256.Sum256(raw)
	set := &sidPinnedSet{digest: base64.RawURLEncoding.EncodeToString(digest[:]), expires: doc.ExpiresAt}
	for _, fp := range fps {
		set.candidates = append(set.candidates, *unique[fp])
	}
	return set, nil
}

// This parser rejects duplicate JSON members (including alg/sub/exp), deep
// objects, and trailing data before JWT parsing can choose an interpretation.
func sidPinnedJSONObject(raw []byte) (map[string]interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value func(int) (interface{}, error)
	value = func(depth int) (interface{}, error) {
		if depth > 32 {
			return nil, errors.New("SID JSON nesting limit exceeded")
		}
		token, e := decoder.Token()
		if e != nil {
			return nil, e
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return token, nil
		}
		switch delimiter {
		case '{':
			m := map[string]interface{}{}
			for decoder.More() {
				k, e := decoder.Token()
				if e != nil {
					return nil, e
				}
				key, ok := k.(string)
				if !ok {
					return nil, errors.New("invalid member")
				}
				if _, exists := m[key]; exists {
					return nil, errors.New("duplicate member")
				}
				v, e := value(depth + 1)
				if e != nil {
					return nil, e
				}
				m[key] = v
			}
			_, e = decoder.Token()
			return m, e
		case '[':
			a := []interface{}{}
			for decoder.More() {
				v, e := value(depth + 1)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
			}
			_, e = decoder.Token()
			return a, e
		default:
			return nil, errors.New("invalid delimiter")
		}
	}
	parsed, e := value(0)
	if e != nil {
		return nil, errors.New("SID JSON invalid or ambiguous")
	}
	if _, e = decoder.Token(); e != io.EOF {
		return nil, errors.New("SID JSON trailing data")
	}
	m, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, errors.New("SID JSON object required")
	}
	return m, nil
}

func sidPinnedKeyForToken(
	ctx context.Context,
	c *config.OIDCAuthConfig,
	token string,
) (*rsa.PublicKey, error) {
	if len(token) > 16<<10 {
		return nil, errors.New("SID ID Token size limit exceeded")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("SID signed JWT required")
	}
	headerBytes, e := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if e != nil {
		return nil, errors.New("SID JWT header invalid")
	}
	header, e := sidPinnedJSONObject(headerBytes)
	if e != nil || header["alg"] != "RS256" {
		return nil, errors.New("SID requires RS256")
	}
	for _, k := range []string{"jku", "jwk", "x5u", "x5c", "crit", "b64"} {
		if _, ok := header[k]; ok {
			return nil, errors.New("SID JWT trust override or extension rejected")
		}
	}
	kid := ""
	if v, present := header["kid"]; present {
		var ok bool
		kid, ok = v.(string)
		if !ok || kid == "" || len(kid) > 128 {
			return nil, errors.New("SID JWT kid invalid")
		}
	}
	payload, e := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if e != nil {
		return nil, errors.New("SID JWT payload invalid")
	}
	if _, e = sidPinnedJSONObject(payload); e != nil {
		return nil, e
	}
	signature, e := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if e != nil || len(signature) < 256 || len(signature) > 512 {
		return nil, errors.New("SID JWT signature size invalid")
	}
	set, e := loadSIDPinnedKeys(c, time.Now())
	if e != nil {
		return nil, e
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	matched, e := uniqueSIDPinnedSignatureMatch(set.candidates, kid, digest[:], signature)
	if e != nil {
		return nil, e
	}
	logger.Infof(
		ctx,
		"sourceid pinned signature matched fingerprint=%s set_sha256=%s "+
			"expires=%s candidates=%d",
		matched.fingerprint,
		set.digest,
		set.expires.UTC().Format(time.RFC3339),
		len(set.candidates),
	)
	return matched.key, nil
}

func uniqueSIDPinnedSignatureMatch(
	candidates []sidPinnedCandidate,
	kid string,
	digest, signature []byte,
) (*sidPinnedCandidate, error) {
	var matched *sidPinnedCandidate
	matches := 0
	for i := range candidates {
		candidate := &candidates[i]
		if kid != "" && !candidate.kids[kid] {
			continue
		}
		if rsa.VerifyPKCS1v15(candidate.key, crypto.SHA256, digest, signature) == nil {
			matched = candidate
			matches++
		}
	}
	if matches != 1 {
		return nil, errors.New("SID ID Token has no unique approved signature match")
	}
	return matched, nil
}
