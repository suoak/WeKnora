package config

import (
	"errors"
	"strings"
	"time"
)

const (
	// SourceIDTLSUserInfoValidation identifies source idtlsuser info validation.
	SourceIDTLSUserInfoValidation = "code_flow_tls_userinfo"
	// SourceIDHTTPCallback preserves the registered internal HTTP callback contract.
	SourceIDHTTPCallback = "http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback"
)

// ValidateSIDTLSUserInfoPolicy provides the validate sidtlsuser info policy operation.
// Only an explicit SID policy can replace signature validation with the
// authenticated, direct Token Endpoint response provenance. No fallback.
func (c *OIDCAuthConfig) ValidateSIDTLSUserInfoPolicy() error {
	return c.validateSIDTLSUserInfoPolicyAt(time.Now())
}

func (c *OIDCAuthConfig) validateSIDTLSUserInfoPolicyAt(now time.Time) error {
	if c.EffectiveSourceIDValidationStrategy() != SourceIDTLSUserInfoValidation ||
		!c.Enable || !c.SourceIDEnable || c.SourceIDProtocolOnly || !c.SourceIDContractConfirmed ||
		!c.SourceIDTLSUserInfoConfirmed || strings.TrimSpace(
		c.SourceIDTLSUserInfoApprovalRef,
	) == "" || len(
		c.SourceIDTLSUserInfoApprovalRef,
	) > 128 {
		return errors.New("SID TLS UserInfo policy approval required")
	}
	switch c.SourceIDTLSUserInfoLifecycle {
	case "", "time_limited":
		if c.SourceIDTLSUserInfoExpiresAt == "" {
			return errors.New("SID TLS UserInfo policy deadline required")
		}
	case "continuous_test":
		// This is an explicit deployment restriction, not remote attestation.
		// It must never be copied into a production deployment.
		if c.SourceIDTLSUserInfoTestInstance != "knowhub-mvp-p3@172.30.232.42" {
			return errors.New("SID continuous policy restricted to designated test instance")
		}
	default:
		return errors.New("SID TLS UserInfo lifecycle invalid")
	}
	// A separately supplied approval expiry remains binding, even in continuous
	// test mode. Removing the plan deadline must not bypass a security expiry.
	if c.SourceIDTLSUserInfoExpiresAt != "" {
		deadline, err := time.Parse(time.RFC3339, c.SourceIDTLSUserInfoExpiresAt)
		if err != nil || !now.Before(deadline) {
			return errors.New("SID TLS UserInfo policy expired or invalid")
		}
	}
	if c.IssuerURL != SourceIDPinnedIssuer || c.RedirectURI != SourceIDHTTPCallback ||
		strings.TrimSpace(c.ClientID) == "" || strings.TrimSpace(c.ClientSecret) == "" ||
		c.ClientAuthMethod != "client_secret_basic" || c.SourceIDClaimsSource != "userinfo" {
		return errors.New("SID TLS UserInfo deployment contract invalid")
	}
	if c.DiscoveryURL != "" &&
		c.DiscoveryURL != SourceIDPinnedIssuer+"/oidc/.well-known/openid-configuration" {
		return errors.New("SID TLS discovery contract invalid")
	}
	for _, pair := range [][2]string{
		{
			c.AuthorizationEndpoint,
			"/oidc/authorize",
		},
		{
			c.TokenEndpoint,
			"/oidc/accessToken",
		},
		{
			c.UserInfoEndpoint,
			"/oidc/profile",
		},
	} {
		if pair[0] != "" && pair[0] != SourceIDPinnedIssuer+pair[1] {
			return errors.New("SID TLS endpoint contract invalid")
		}
	}
	switch c.UserInfoTransport {
	case "", "bearer_get", "form_post", "multipart_post":
	default:
		return errors.New("SID UserInfo transport invalid")
	}
	return nil
}
