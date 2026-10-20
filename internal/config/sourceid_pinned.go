package config

import (
	"errors"
	"path/filepath"
	"strings"
	"time"
)

const (
	// SourceIDJWKSValidation identifies source idjwksvalidation.
	SourceIDJWKSValidation = "jwks_signature"
	// SourceIDPinnedValidation selects explicitly approved SID runtime signing keys.
	SourceIDPinnedValidation = "sid_pinned_keys"
	// SourceIDPinnedIssuer is the sole issuer allowed to use the SID-specific policies.
	SourceIDPinnedIssuer = "https://sid.ruijie.com.cn"
)

// EffectiveSourceIDValidationStrategy provides the effective source idvalidation strategy operation.
func (c *OIDCAuthConfig) EffectiveSourceIDValidationStrategy() string {
	if c.SourceIDValidationStrategy == "" {
		return SourceIDJWKSValidation
	}
	return c.SourceIDValidationStrategy
}

// ValidateSourceIDValidationStrategy provides the validate source idvalidation strategy operation.
func (c *OIDCAuthConfig) ValidateSourceIDValidationStrategy() error {
	switch c.EffectiveSourceIDValidationStrategy() {
	case SourceIDJWKSValidation:
		return nil
	case SourceIDTLSUserInfoValidation:
		return c.ValidateSIDTLSUserInfoPolicy()
	case SourceIDPinnedValidation:
	default:
		return errors.New("unsupported OIDC validation strategy")
	}
	if !c.SourceIDEnable || c.IssuerURL != SourceIDPinnedIssuer || strings.TrimSpace(c.ClientID) == "" {
		return errors.New("pinned keys are restricted to SID")
	}
	if !c.SourceIDPinnedKeysConfirmed || strings.TrimSpace(c.SourceIDPinnedKeysApprovalRef) == "" ||
		len(c.SourceIDPinnedKeysApprovalRef) > 128 {
		return errors.New("SID pinned key approval required")
	}
	if !filepath.IsAbs(c.SourceIDPinnedKeysFile) || c.SourceIDPinnedKeyFingerprints == "" {
		return errors.New("SID approved runtime key file and fingerprints required")
	}
	if _, e := time.Parse(time.RFC3339, c.SourceIDPinnedKeysExpiresAt); e != nil {
		return errors.New("SID pinned key fixed expiry required")
	}
	return nil
}
