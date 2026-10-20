package config

import (
	"errors"
	"strings"
	"time"
)

// EffectiveSourceIDAccountScope provides the effective source idaccount scope operation.
func (c *OIDCAuthConfig) EffectiveSourceIDAccountScope() string {
	if c.SourceIDAccountScope == "" {
		return "pilot"
	}
	return c.SourceIDAccountScope
}

// SourceIDAccountPolicy provides the source idaccount policy operation.
func (c *OIDCAuthConfig) SourceIDAccountPolicy() error {
	if c.EffectiveSourceIDAccountLinkMode() != "trusted_ownership" &&
		c.EffectiveSourceIDAccountLinkMode() != "verified_email_policy" {
		return errors.New("sourceid account link mode invalid")
	}
	if c.EffectiveSourceIDAccountLinkMode() == "verified_email_policy" &&
		(c.EffectiveSourceIDAccountScope() != "enterprise_all" ||
			!c.SourceIDVerifiedEmailPolicyConfirmed ||
			strings.TrimSpace(
				c.SourceIDVerifiedEmailPolicyRef,
			) == "") {
		return errors.New("sourceid verified email policy pending")
	}
	switch c.EffectiveSourceIDAccountScope() {
	case "pilot":
		if strings.TrimSpace(c.SourceIDPilotAccountsFile) == "" {
			return errors.New("sourceid pilot ownership unavailable")
		}
	case "enterprise_all":
		if c.EffectiveSourceIDAccountLinkMode() == "trusted_ownership" &&
			(!c.SourceIDEnterprisePolicyConfirmed || strings.TrimSpace(c.SourceIDEnterprisePolicyRef) == "") {
			return errors.New("sourceid enterprise ownership policy pending")
		}
		if _, err := c.SourceIDEnterpriseFreshness(); c.EffectiveSourceIDAccountLinkMode() == "trusted_ownership" &&
			err != nil {
			return err
		}
	default:
		return errors.New("sourceid account scope invalid")
	}
	if c.SourceIDJITEnable {
		if c.EffectiveSourceIDAccountScope() != "enterprise_all" || !c.SourceIDJITPolicyConfirmed ||
			strings.TrimSpace(c.SourceIDJITPolicyRef) == "" ||
			len(c.SourceIDJITDomains()) == 0 {
			return errors.New("sourceid jit policy pending")
		}
	}
	return nil
}

// EffectiveSourceIDAccountLinkMode provides the effective source idaccount link mode operation.
func (c *OIDCAuthConfig) EffectiveSourceIDAccountLinkMode() string {
	if c.SourceIDAccountLinkMode == "" {
		return "trusted_ownership"
	}
	return c.SourceIDAccountLinkMode
}

// SourceIDJITDomains provides the source idjitdomains operation.
// Exact corporate domains, approved with the independent JIT identity and
// eligibility contract. No wildcard/substring domain matching.
func (c *OIDCAuthConfig) SourceIDJITDomains() []string {
	var result []string
	for _, raw := range strings.Split(c.SourceIDJITEmailDomains, ",") {
		domain := strings.ToLower(strings.TrimSpace(raw))
		if domain == "" || !strings.Contains(domain, ".") || strings.ContainsAny(domain, "@*/ :\\\t\r\n") {
			return nil
		}
		for _, ch := range domain {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' && ch != '.' {
				return nil
			}
		}
		result = append(result, domain)
	}
	return result
}

// SourceIDEnterpriseFreshness provides the source identerprise freshness operation.
func (c *OIDCAuthConfig) SourceIDEnterpriseFreshness() (time.Duration, error) {
	d, err := time.ParseDuration(c.SourceIDEnterpriseMaxAge)
	if err != nil || d < time.Second {
		return 0, errors.New("sourceid enterprise freshness policy pending")
	}
	return d, nil
}
