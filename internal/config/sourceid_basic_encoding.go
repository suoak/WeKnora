package config

import "fmt"

// EffectiveSourceIDBasicEncoding never changes non-SourceID providers.
func (c *OIDCAuthConfig) EffectiveSourceIDBasicEncoding() (string, error) {
	if !c.SourceIDEnable || c.SourceIDBasicEncoding == "" {
		return "oauth_form", nil
	}
	switch c.SourceIDBasicEncoding {
	case "oauth_form", "raw":
		return c.SourceIDBasicEncoding, nil
	default:
		return "", fmt.Errorf("sourceid_basic_encoding must be oauth_form or raw")
	}
}
