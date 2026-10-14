package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/google/uuid"
)

type sourceIDDiagnosticContext struct {
	ID          string
	Started     time.Time
	StatePassed bool
}
type sourceIDDiagnosticKey struct{}

// Token diagnostics are bounded per process independently of Docker rotation.
// Exhaustion changes observability only, never authentication or HTTP behavior.
var sourceIDTokenDiagnosticBytes atomic.Uint64
var sourceIDDiagnosticWindowStarted atomic.Int64
var sourceIDDiagnosticLimitNotified atomic.Bool

const sourceIDTokenDiagnosticBudget = 10 * 1024 * 1024

func sourceIDEmitTokenDiagnostic(ctx context.Context, data []byte) {
	if len(data) > 32768 {
		return
	}
	now := time.Now().Unix()
	sourceIDDiagnosticWindowStarted.CompareAndSwap(0, now)
	if now-sourceIDDiagnosticWindowStarted.Load() > 30*60 {
		if sourceIDDiagnosticLimitNotified.CompareAndSwap(false, true) {
			logger.Warn(ctx, `{"event":"sourceid_diagnostics_limit","reason":"window_expired"}`)
		}
		return
	}
	for {
		used := sourceIDTokenDiagnosticBytes.Load()
		if used+uint64(len(data)) > sourceIDTokenDiagnosticBudget {
			if sourceIDDiagnosticLimitNotified.CompareAndSwap(false, true) {
				logger.Warn(ctx, `{"event":"sourceid_diagnostics_limit","reason":"byte_budget"}`)
			}
			return
		}
		if sourceIDTokenDiagnosticBytes.CompareAndSwap(used, used+uint64(len(data))) {
			logger.Warn(ctx, string(data))
			return
		}
	}
}

// NewSourceIDDiagnosticContext uses a fresh server-owned identifier. It never
// imports state, code, subject, request headers or a browser supplied trace ID.
func NewSourceIDDiagnosticContext(ctx context.Context, enabled bool) context.Context {
	if !enabled {
		return ctx
	}
	return context.WithValue(ctx, sourceIDDiagnosticKey{}, sourceIDDiagnosticContext{ID: uuid.NewString(), Started: time.Now()})
}

// Called only after the handler's state and durable transaction checks pass.
func SourceIDDiagnosticStatePassed(ctx context.Context) context.Context {
	entry, ok := ctx.Value(sourceIDDiagnosticKey{}).(sourceIDDiagnosticContext)
	if !ok {
		return ctx
	}
	entry.StatePassed = true
	return context.WithValue(ctx, sourceIDDiagnosticKey{}, entry)
}

func sourceIDTokenDiagnosticPhase(ctx context.Context, event string, request *OIDCProbeHTTPDiagnostics, transport *sourceIDTokenTelemetry) {
	if event != "sourceid_token_request" && event != "sourceid_token_send" {
		return
	}
	data, err := json.Marshal(struct {
		Event     string                    `json:"event"`
		Trace     string                    `json:"trace"`
		Request   *OIDCProbeHTTPDiagnostics `json:"diagnostics,omitempty"`
		Transport *sourceIDTokenTelemetry   `json:"transport"`
	}{event, transport.Trace, request, transport})
	if err == nil && len(data) <= 32768 {
		sourceIDEmitTokenDiagnostic(ctx, data)
	}
}

func sourceIDHTTPDiagnostics(ctx context.Context, report *OIDCProbeReport) {
	entry, ok := ctx.Value(sourceIDDiagnosticKey{}).(sourceIDDiagnosticContext)
	if !ok {
		return
	}
	data, _ := json.Marshal(struct {
		Event string                `json:"event"`
		Trace string                `json:"trace"`
		HTTP  []OIDCProbeHTTPResult `json:"http"`
	}{"sourceid_token_http", entry.ID, report.HTTP})
	if len(data) <= 32768 {
		sourceIDEmitTokenDiagnostic(ctx, data)
	} else {
		sourceIDEmitTokenDiagnostic(ctx, []byte(`{"event":"sourceid_token_http","diagnostic_suppressed":"size_limit"}`))
	}
}

func sourceIDTokenOutcome(row OIDCProbeHTTPResult) string {
	if row.Status == 0 {
		return "network_or_tls_error"
	}
	if row.Diagnostics == nil || row.Diagnostics.Response == nil {
		return "UNKNOWN"
	}
	r := row.Diagnostics.Response
	if row.Status == 401 && r.BodyKind == "empty" && r.WWWPresent {
		for _, s := range r.WWWSchemes {
			if s == "basic" {
				return "empty_401_basic_challenge"
			}
		}
	}
	if r.OAuthError != "" {
		return "standard_oauth_error"
	}
	if r.BodyKind == "html" {
		return "html_response_origin_unknown"
	}
	if row.Status == 200 {
		if r.BodyKind != "json_object" {
			return "invalid_token_response_structure"
		}
		if r.ErrorFields["access_token"] != "string" && r.ErrorFields["id_token"] != "string" {
			return "missing_token_fields"
		}
		return "token_fields_present_verification_pending"
	}
	return "http_error_origin_unknown"
}

func sourceIDSchemaDiagnostic(ctx context.Context, stage string, claims map[string]interface{}) {
	entry, ok := ctx.Value(sourceIDDiagnosticKey{}).(sourceIDDiagnosticContext)
	if !ok || (stage != "id_token" && stage != "userinfo") {
		return
	}
	budget := 128
	data, _ := json.Marshal(struct {
		Event  string                 `json:"event"`
		Trace  string                 `json:"trace"`
		Stage  string                 `json:"stage"`
		Schema map[string]interface{} `json:"schema"`
	}{"sourceid_verified_schema", entry.ID, stage, probeSchema(claims, 0, &budget)})
	sourceIDEmitTokenDiagnostic(ctx, data)
}

func SourceIDDiagnosticStage(ctx context.Context, stage string, start time.Time, err error) {
	entry, ok := ctx.Value(sourceIDDiagnosticKey{}).(sourceIDDiagnosticContext)
	if !ok {
		return
	}
	allowed := map[string]bool{"authorize": true, "callback_state": true, "token": true, "id_token": true, "userinfo": true, "identity_mapping": true, "local_session": true, "callback_result": true}
	if !allowed[stage] {
		return
	}
	category := "none"
	if err != nil {
		category = "rejected"
		// Match only code-owned prefixes. Arbitrary provider/SQL messages are
		// never emitted, even if they contain a plausible public error string.
		for prefix, label := range map[string]string{"OIDC token exchange failed: status=401": "token_http_401", "OIDC ID Token nonce mismatch": "nonce_mismatch", "sourceid enrollment requires review": "enrollment_required", "sourceid identity conflict": "identity_conflict", "sourceid employee disabled": "account_disabled", "sourceid employee or session missing": "identity_fields_missing", "OIDC UserInfo subject mismatch": "subject_mismatch"} {
			if err.Error() == prefix {
				category = label
			}
		}
		if strings.HasPrefix(err.Error(), "id_token verification failed:") {
			category = "id_token_verification"
		}
	}
	data, _ := json.Marshal(struct {
		Event    string `json:"event"`
		Trace    string `json:"trace"`
		Stage    string `json:"stage"`
		Pass     bool   `json:"pass"`
		Duration int64  `json:"duration_ms"`
		Category string `json:"category"`
	}{"sourceid_stage", entry.ID, stage, err == nil, time.Since(start).Milliseconds(), category})
	sourceIDEmitTokenDiagnostic(ctx, data)
}
