package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
)

type sourceIDReauthRequestKey struct{}

// Stored in the existing authorization transaction Result field, never in
// browser-controlled state. Login-result rows use a separate hashed key space.
type sourceIDReauthRequest struct {
	TLSBinding string    `json:"tls_binding,omitempty"`
	Version    int       `json:"version"`
	Prompt     string    `json:"prompt"`
	Started    time.Time `json:"started"`
}

func sourceIDAuthorizationMetadata(cfg *config.OIDCAuthConfig, query url.Values) (string, error) {
	m := sourceIDReauthRequest{Version: 1, Started: time.Now().UTC()}
	if sidTLSUserInfoMode(cfg) {
		binding := sidTLSConfigurationBinding(cfg)
		m.TLSBinding = hex.EncodeToString(binding[:])
	}
	if cfg.SourceIDForceReauthPromptLogin {
		m.Prompt = "login"
		query.Set("prompt", "login")
		if _, err := sourceIDForcedAuthorizationURL(cfg.AuthorizationEndpoint, query); err != nil {
			return "", err
		}
	}
	b, err := json.Marshal(m)
	return string(b), err
}

func sourceIDForcedAuthorizationURL(endpoint string, query url.Values) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", errors.New("sourceid authorization endpoint invalid")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || q.Has("max_age") || query.Has("max_age") {
		return "", errors.New("sourceid authorization parameters invalid")
	}
	for k, values := range query {
		q.Del(k)
		for _, value := range values {
			q.Add(k, value)
		}
	}
	q.Set("prompt", "login") // Replace endpoint defaults, including duplicates.
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func prepareSourceIDReauthentication(ctx context.Context, cfg *config.OIDCAuthConfig, saved string) (context.Context, error) {
	var request sourceIDReauthRequest
	if saved != "" {
		if err := json.Unmarshal([]byte(saved), &request); err != nil {
			return ctx, errors.New("sourceid reauthentication transaction invalid")
		}
	}
	if cfg.SourceIDForceReauthPromptLogin && (request.Version != 1 || request.Prompt != "login" || request.Started.IsZero() || request.Started.After(time.Now())) {
		return ctx, errors.New("sourceid forced reauthentication transaction required")
	}
	return context.WithValue(ctx, sourceIDReauthRequestKey{}, request), nil
}

// Only called with signature/issuer/audience/time/nonce-verified ID Token
// claims, BEFORE UserInfo merging. No UserInfo field can supply this evidence.
func checkSourceIDReauthentication(ctx context.Context, cfg *config.OIDCAuthConfig, signed map[string]interface{}) error {
	if !cfg.SourceIDEnable || !cfg.SourceIDForceReauthPromptLogin {
		return nil
	}
	request, _ := ctx.Value(sourceIDReauthRequestKey{}).(sourceIDReauthRequest)
	if request.Version != 1 || request.Prompt != "login" || request.Started.IsZero() {
		return errors.New("sourceid forced reauthentication transaction required")
	}
	value, present := signed["auth_time"]
	evidence := "NOT_PROVIDED_CONTRACT_REQUIRED"
	var err error
	if present {
		var seconds float64
		switch n := value.(type) {
		case float64:
			seconds = n
		case json.Number:
			seconds, err = n.Float64()
		default:
			err = errors.New("sourceid auth_time invalid")
		}
		if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || math.Trunc(seconds) != seconds || seconds < float64(request.Started.Unix()) || seconds > float64(time.Now().Unix()) {
			evidence = "INVALID_OR_STALE"
			err = errors.New("sourceid reauthentication auth_time invalid or stale")
		} else {
			evidence = "SIGNED_AUTH_TIME_VERIFIED"
			if sidTLSUserInfoMode(cfg) {
				evidence = "TLS_TOKEN_AUTH_TIME_VERIFIED"
			}
		}
	}
	if entry, ok := ctx.Value(sourceIDDiagnosticKey{}).(sourceIDDiagnosticContext); ok {
		// Fixed enum and server trace only; never a claim value or identity.
		data, _ := json.Marshal(struct {
			Event    string `json:"event"`
			Trace    string `json:"trace"`
			Evidence string `json:"evidence"`
		}{"sourceid_reauthentication", entry.ID, evidence})
		sourceIDEmitTokenDiagnostic(ctx, data)
	}
	return err
}
