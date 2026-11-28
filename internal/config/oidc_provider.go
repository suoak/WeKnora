package config

import (
	"errors"
	"strings"
)

// ErrOIDCProviderUnavailable identifies a provider failure without granting an
// alternate authentication path or affecting independently valid sessions.
var ErrOIDCProviderUnavailable = errors.New("OIDC provider temporarily unavailable")

// ValidateOIDCProviderConfig is the shared strict provider gate for preflight,
// login availability and authorization attempts. It never mutates configuration.
func ValidateOIDCProviderConfig(c *OIDCAuthConfig) error {
	if c == nil {
		return nil
	}
	if err := c.ValidateSourceIDValidationStrategy(); err != nil {
		return err
	}
	if _, err := c.EffectiveSourceIDBasicEncoding(); err != nil {
		return err
	}
	if !c.Enable {
		return nil
	}
	if strings.TrimSpace(c.ClientID) == "" || strings.TrimSpace(c.ClientSecret) == "" {
		return errors.New("OIDC provider client credentials required")
	}
	if strings.TrimSpace(c.DiscoveryURL) == "" &&
		(strings.TrimSpace(c.AuthorizationEndpoint) == "" || strings.TrimSpace(c.TokenEndpoint) == "") {
		return errors.New("OIDC discovery or authorization and token endpoints required")
	}
	return nil
}
