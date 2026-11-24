package feishusafety

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrProviderFailure rejects incomplete provider output without withdrawing
// independent sources' authority or forgiving the request's RPM/TPM charge.
var ErrProviderFailure = errors.New("embedding provider request failed")

// ProviderResponseError retains the SDK cause for internal classification, but
// its user-facing message never includes a response body or credentials.
type ProviderResponseError struct {
	Status int
	Cause  error
}

func (e *ProviderResponseError) Error() string {
	return fmt.Sprintf("embedding provider returned HTTP %d", e.Status)
}
func (e *ProviderResponseError) Unwrap() []error { return []error{ErrProviderFailure, e.Cause} }

// SetProviderFailureIsolation is called by the automatic controller only after
// verifying signed normal-service authorization. It grants no outbound permit.
func (g *Guard) SetProviderFailureIsolation(enabled bool) { g.providerFailureIsolation.Store(enabled) }

// ProviderFailuresIsolated reports the verified normal service's failure policy.
func (g *Guard) ProviderFailuresIsolated() bool {
	return g != nil && g.providerFailureIsolation.Load() && !g.localClosed.Load()
}

// ProviderFailuresIsolated applies normal-service isolation only to marked work.
func ProviderFailuresIsolated(ctx context.Context) bool {
	return Scoped(ctx) && Default().ProviderFailuresIsolated()
}

func (g *Guard) recordProviderFailure(ctx context.Context, reason, evidence string) bool {
	if !g.ProviderFailuresIsolated() {
		return false
	}
	audit, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	err := g.client.XAdd(audit, &redis.XAddArgs{
		Stream: g.key("audit"), MaxLen: 1000, Approx: true,
		Values: map[string]any{"event": "provider_failure", "reason": reason, "http_evidence": evidence},
	}).Err()
	if err != nil {
		// Audit uncertainty denies further work until the existing controller
		// completes health checks. It cannot turn one provider failure into a
		// permanent installation-wide emergency.
		g.SuspendDependency(true)
		_ = g.client.Del(audit, g.PermitKey()).Err()
	}
	return true
}

func providerRetryFailure(ctx context.Context, g *Guard, reason string) error {
	if g.recordProviderFailure(ctx, reason, "") {
		return fmt.Errorf("%w: %s", ErrProviderFailure, reason)
	}
	trip(g, reason)
	return ErrClosed
}

// RecoverableProviderStop accepts only an old, evidenced HTTP provider stop. Unknown stops,
// caller-side validation/accounting failures and explicit operator stops do not
// acquire automatic recovery semantics.
func (g *Guard) RecoverableProviderStop(reason, raw string) bool {
	if !g.ProviderFailuresIsolated() {
		return false
	}
	var evidence httpStopEvidence
	if json.Unmarshal([]byte(raw), &evidence) != nil || evidence.Family != "embedding" ||
		evidence.Operation != "embedding" || evidence.Status < 400 || evidence.Status > 599 ||
		reason != fmt.Sprintf("embedding_http_%d", evidence.Status) || evidence.SourceHash == "" {
		return false
	}
	endpoint, err := url.Parse("//" + g.config.EndpointHost)
	return err == nil && evidence.EndpointHash == httpAuditDigest(endpoint.Hostname())
}
