package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSIDPinnedConfig(t *testing.T) {
	if got := (&OIDCAuthConfig{}).EffectiveSourceIDValidationStrategy(); got != SourceIDJWKSValidation {
		t.Fatal("default must remain strict JWKS")
	}
	for _, name := range []string{"valid", "other_provider", "missing_approval", "missing_file", "missing_fingerprints", "missing_expiry", "unknown_strategy"} {
		t.Run(name, func(t *testing.T) {
			c := &OIDCAuthConfig{SourceIDEnable: true, IssuerURL: SourceIDPinnedIssuer, ClientID: "synthetic", SourceIDValidationStrategy: SourceIDPinnedValidation, SourceIDPinnedKeysFile: filepath.Join(t.TempDir(), "keys.json"), SourceIDPinnedKeysConfirmed: true, SourceIDPinnedKeysApprovalRef: "SYNTHETIC", SourceIDPinnedKeysExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339), SourceIDPinnedKeyFingerprints: "synthetic-runtime-validated"}
			switch name {
			case "other_provider":
				c.SourceIDEnable = false
			case "missing_approval":
				c.SourceIDPinnedKeysConfirmed = false
			case "missing_file":
				c.SourceIDPinnedKeysFile = ""
			case "missing_fingerprints":
				c.SourceIDPinnedKeyFingerprints = ""
			case "missing_expiry":
				c.SourceIDPinnedKeysExpiresAt = ""
			case "unknown_strategy":
				c.SourceIDValidationStrategy = "none"
			}
			if (c.ValidateSourceIDValidationStrategy() == nil) != (name == "valid") {
				t.Fatal("unexpected policy validation")
			}
		})
	}
}

func TestSIDPinnedEnvironment(t *testing.T) {
	t.Setenv("SOURCEID_VALIDATION_STRATEGY", SourceIDPinnedValidation)
	t.Setenv("SOURCEID_PINNED_KEYS_FILE", "/run/controlled/sid.json")
	t.Setenv("SOURCEID_PINNED_KEYS_CONFIRMED", "true")
	t.Setenv("SOURCEID_PINNED_KEYS_APPROVAL_REF", "SYNTHETIC")
	t.Setenv("SOURCEID_PINNED_KEYS_EXPIRES_AT", "2026-10-20T00:00:00Z")
	t.Setenv("SOURCEID_PINNED_KEY_FINGERPRINTS", "SYNTHETIC-FP")
	c := &Config{}
	applyOIDCEnvOverrides(c)
	if c.OIDCAuth.SourceIDValidationStrategy != SourceIDPinnedValidation || c.OIDCAuth.SourceIDPinnedKeysFile != "/run/controlled/sid.json" || !c.OIDCAuth.SourceIDPinnedKeysConfirmed || c.OIDCAuth.SourceIDPinnedKeysApprovalRef != "SYNTHETIC" || c.OIDCAuth.SourceIDPinnedKeyFingerprints != "SYNTHETIC-FP" {
		t.Fatal("controlled environment fields not loaded")
	}
}
