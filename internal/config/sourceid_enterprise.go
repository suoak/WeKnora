package config

import (
	"errors"
	"strings"
	"time"
)

func (c *OIDCAuthConfig) EffectiveSourceIDAccountScope() string {
	if c.SourceIDAccountScope == "" {
		return "pilot"
	}
	return c.SourceIDAccountScope
}

func (c *OIDCAuthConfig) SourceIDAccountPolicy() error {
	switch c.EffectiveSourceIDAccountScope() {
	case "pilot":
		if strings.TrimSpace(c.SourceIDPilotAccountsFile) == "" {
			return errors.New("sourceid pilot ownership unavailable")
		}
	case "enterprise_all":
		if !c.SourceIDEnterprisePolicyConfirmed || strings.TrimSpace(c.SourceIDEnterprisePolicyRef) == "" {
			return errors.New("sourceid enterprise ownership policy pending")
		}
		if _, err := c.SourceIDEnterpriseFreshness(); err != nil {
			return err
		}
	default:
		return errors.New("sourceid account scope invalid")
	}
	if c.SourceIDJITEnable {
		if c.EffectiveSourceIDAccountScope() != "enterprise_all" || !c.SourceIDJITPolicyConfirmed || strings.TrimSpace(c.SourceIDJITPolicyRef) == "" || len(c.SourceIDJITDomains()) == 0 {
			return errors.New("sourceid jit policy pending")
		}
	}
	return nil
}

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
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '.') {
				return nil
			}
		}
		result = append(result, domain)
	}
	return result
}

func (c *OIDCAuthConfig) SourceIDEnterpriseFreshness() (time.Duration, error) {
	d, err := time.ParseDuration(c.SourceIDEnterpriseMaxAge)
	if err != nil || d < time.Second {
		return 0, errors.New("sourceid enterprise freshness policy pending")
	}
	return d, nil
}
