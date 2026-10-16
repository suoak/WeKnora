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
	// JIT remains a separately approved product capability, not a fallback when
	// historical account ownership cannot be established.
	if c.SourceIDJITEnable {
		return errors.New("sourceid jit policy unsupported")
	}
	return nil
}

func (c *OIDCAuthConfig) SourceIDEnterpriseFreshness() (time.Duration, error) {
	d, err := time.ParseDuration(c.SourceIDEnterpriseMaxAge)
	if err != nil || d < time.Second {
		return 0, errors.New("sourceid enterprise freshness policy pending")
	}
	return d, nil
}
