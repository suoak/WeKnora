package config

import "testing"

func TestSourceIDBasicEncodingConfig(t *testing.T) {
	for _, tc := range []struct {
		sourceID    bool
		value, want string
		invalid     bool
	}{
		{true, "", "oauth_form", false},
		{true, "oauth_form", "oauth_form", false},
		{true, "raw", "raw", false},
		{true, "invalid", "", true},
		{false, "raw", "oauth_form", false},
		{false, "invalid", "oauth_form", false},
	} {
		c := &OIDCAuthConfig{SourceIDEnable: tc.sourceID, SourceIDBasicEncoding: tc.value}
		got, err := c.EffectiveSourceIDBasicEncoding()
		if got != tc.want || (err != nil) != tc.invalid {
			t.Fatal("unexpected effective Basic mode")
		}
		if (ValidateConfig(&Config{OIDCAuth: c}) != nil) != tc.invalid {
			t.Fatal("startup validation differs from runtime")
		}
	}
	t.Setenv("SOURCEID_ENABLE", "true")
	t.Setenv("SOURCEID_BASIC_ENCODING", "raw")
	cfg := &Config{}
	applyOIDCEnvOverrides(cfg)
	if got, err := cfg.OIDCAuth.EffectiveSourceIDBasicEncoding(); got != "raw" || err != nil {
		t.Fatal("environment mode not applied")
	}
}
