package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
)

type (
	apiObserverKey     struct{}
	userRefreshHintKey struct{}
)

// APIObservation contains only content-free evidence from one upstream request.
// RequestID is available to an operator probe in memory, never serialized by
// this type; shared audit/log output uses its irreversible correlation hash.
type APIObservation struct {
	Operation      string        `json:"operation"`
	Category       ErrorCategory `json:"category"`
	HTTPStatus     int           `json:"http_status"`
	Code           int           `json:"code"`
	RequestID      string        `json:"-"`
	RequestIDHash  string        `json:"request_id_hash,omitempty"`
	RequiredScopes []string      `json:"required_scopes,omitempty"`
	OAuthError     string        `json:"oauth_error,omitempty"`
}

// WithAPIObserver adds request diagnostics without modifying identity or policy.
func WithAPIObserver(ctx context.Context, observer func(APIObservation)) context.Context {
	return context.WithValue(ctx, apiObserverKey{}, observer)
}

// UserTokenRefreshFingerprint tells the persisted user-token provider that the
// upstream rejected this exact access-token generation. It grants no resource
// access and never carries a plaintext token in a context or log.
func UserTokenRefreshFingerprint(ctx context.Context) string {
	value, _ := ctx.Value(userRefreshHintKey{}).(string)
	return value
}

func tokenFingerprint(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

var requiredScopePattern = regexp.MustCompile(
	`\b(?:docs|drive|docx|wiki|sheets|bitable):[a-z][a-z0-9_.:]{1,96}\b`,
)

// ResponseRequestID accepts only bounded opaque provider correlation IDs.
func ResponseRequestID(headers http.Header) string {
	for _, name := range []string{"X-Tt-Logid", "X-Request-Id", "X-Lark-Request-Id"} {
		if value := headers.Get(name); requestIDPattern.MatchString(value) {
			return value
		}
	}
	return ""
}

func requiredScopes(message string) []string {
	var result []string
	seen := map[string]bool{}
	for _, scope := range requiredScopePattern.FindAllString(message, -1) {
		if !seen[scope] {
			seen[scope] = true
			result = append(result, scope)
		}
	}
	return result
}

// RecordAPIObservation reports content-free diagnostics to the bound observer.
func RecordAPIObservation(ctx context.Context, observation APIObservation) {
	if observation.RequestID != "" {
		observation.RequestIDHash = requestIDHash(observation.RequestID)
	}
	if observer, ok := ctx.Value(apiObserverKey{}).(func(APIObservation)); ok && observer != nil {
		observer(observation)
	}
}

func requestIDHash(value string) string {
	if value == "" {
		return ""
	}
	return tokenFingerprint(value)[:12]
}
