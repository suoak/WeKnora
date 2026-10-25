package types

import "errors"

// FeishuCredentialRef is the V2 credential contract. It contains references only,
// never bearer tokens. Existing FeishuConnection rows remain OAuth-only until
// the separately reviewed additive migration is applied.
type FeishuCredentialRef struct {
	Version       int    `json:"version"`
	Kind          string `json:"kind"`
	ConnectionID  string `json:"connection_id"`
	AppID         string `json:"app_id"`
	OwnerID       string `json:"owner_id,omitempty"`
	OpenID        string `json:"-"`
	GrantEpoch    string `json:"-"`
	ConfigVersion string `json:"-"`
}

var (
	// ErrFeishuIdentityReference rejects malformed or foreign credential references.
	ErrFeishuIdentityReference = errors.New("feishu_identity_reference_invalid")
	// ErrFeishuIdentityDisabled rejects an identity whose capability is disabled.
	ErrFeishuIdentityDisabled = errors.New("feishu_identity_disabled")
	// ErrFeishuIdentityRequired requires callers to select an explicit V2 identity.
	ErrFeishuIdentityRequired = errors.New("feishu_identity_required")
)

// Validate checks a server-loaded reference against the authenticated owner.
// This is structural validation, not a substitute for live connection, epoch,
// application config, KB grant, or source ACL checks by the existing service.
func (r FeishuCredentialRef) Validate(callerID string) error {
	if r.Version != 1 || r.ConnectionID == "" || r.AppID == "" || r.ConfigVersion == "" || callerID == "" {
		return ErrFeishuIdentityReference
	}
	switch r.Kind {
	case FeishuUserOAuth:
		if r.OwnerID != callerID || r.OpenID == "" || r.GrantEpoch == "" {
			return ErrFeishuIdentityReference
		}
	case FeishuApplicationGrant:
		if r.OwnerID != "" || r.OpenID != "" || r.GrantEpoch != "" {
			return ErrFeishuIdentityReference
		}
	default:
		return ErrFeishuIdentityReference
	}
	return nil
}

// FeishuAccessCapabilities is a default-off policy for the future V2 adapter.
// It is deliberately not wired into legacy routes in the audit/PoC tranche.
// Rollout and both identities must be explicitly enabled by the server.
type FeishuAccessCapabilities struct {
	RolloutEnabled     bool `json:"-"`
	ApplicationEnabled bool `json:"application_enabled"`
	PersonalEnabled    bool `json:"personal_enabled"`
}

// ResolveMode requires an explicit identity even if only one is enabled. A
// missing or expired user grant must never select application access instead.
func (p FeishuAccessCapabilities) ResolveMode(requested string) (string, error) {
	if requested != FeishuUserOAuth && requested != FeishuApplicationGrant {
		return "", ErrFeishuIdentityRequired
	}
	if !p.RolloutEnabled || requested == FeishuUserOAuth && !p.PersonalEnabled ||
		requested == FeishuApplicationGrant && !p.ApplicationEnabled {
		return "", ErrFeishuIdentityDisabled
	}
	return requested, nil
}
