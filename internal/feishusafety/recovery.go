package feishusafety

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// RecoveryContext is server-owned execution identity, never an HTTP parameter.
// Its presence is not authority: Redis must bind the same source/config/run to
// a separately approved recovery state, in addition to the ordinary permit.
type RecoveryContext struct {
	Plan, Config, Run string
	Generation        int64
}
type (
	recoveryContextKey  struct{}
	recoveryOutboundKey struct{}
)

// RecoveryOutboundExecution distinguishes actual HTTP/budget use from the
// producer's pre-enqueue check. Only this package marks an outbound operation.
func RecoveryOutboundExecution(ctx context.Context) (RecoveryContext, bool) {
	required, _ := ctx.Value(recoveryOutboundKey{}).(bool)
	return recoveryIdentity(ctx), required
}

// WithRecoveryContext attaches server-owned identity; it does not grant authority.
func WithRecoveryContext(ctx context.Context, identity RecoveryContext) context.Context {
	return context.WithValue(ctx, recoveryContextKey{}, identity)
}

func recoveryIdentity(ctx context.Context) RecoveryContext {
	i, _ := ctx.Value(recoveryContextKey{}).(RecoveryContext)
	return i
}

// WithRecoveryExecution binds identity to the actual source execution.
func WithRecoveryExecution(ctx context.Context, config, run string, generation int64) context.Context {
	i := recoveryIdentity(ctx)
	i.Config = config
	i.Run = run
	i.Generation = generation
	return WithRecoveryContext(ctx, i)
}

// SourceConfigFingerprint canonicalizes an already-authorized server snapshot.
// It returns no credentials; changing source identity/config invalidates consent.
func SourceConfigFingerprint(source, kb, kind, state string, tenant uint64, config []byte) (string, error) {
	if state == "active" || state == "unmanaged" {
		state = "admissible"
	}
	var canonical any
	if len(config) == 0 {
		config = []byte(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()
	if err := decoder.Decode(&canonical); err != nil {
		return "", err
	}
	b, err := json.Marshal([]any{source, kb, kind, state, tenant, canonical})
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// Fresh creates another closed-by-permit lifecycle. It does not reopen this
// object's local latch or install the replacement in any running process.
func (g *Guard) Fresh() (*Guard, error) {
	fresh, err := New(g.client, g.config)
	if err == nil {
		fresh.approvedSourceValidator.Store(g.approvedSourceValidator.Load())
	}
	return fresh, err
}

type approvedSourceValidator struct {
	check func(context.Context, string, string) error
}

// SetApprovedSourceValidator installs a server-owned live authority check.
// Neither a task payload nor a request header can supply this validator.
func (g *Guard) SetApprovedSourceValidator(check func(context.Context, string, string) error) {
	if check == nil {
		g.approvedSourceValidator.Store(nil)
		return
	}
	g.approvedSourceValidator.Store(&approvedSourceValidator{check: check})
}

func (g *Guard) checkApprovedSource(
	ctx context.Context,
	source string,
	identity RecoveryContext,
) (bool, error) {
	v := g.approvedSourceValidator.Load()
	if v == nil {
		return false, nil
	}
	if err := v.check(ctx, source, identity.Config); err != nil {
		return true, ErrClosed
	}
	return true, nil
}

// RequireRecovery pins the replacement lifecycle closed if distributed recovery state is lost.
func (g *Guard) RequireRecovery() { g.recoveryRequired.Store(true) }
