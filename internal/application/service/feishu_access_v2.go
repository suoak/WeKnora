package service

import (
	"context"
	"errors"
	"os"

	"github.com/Tencent/WeKnora/internal/types"
)

func (c *FeishuAppConfig) identityVersion(mode string) string {
	if c.Capabilities != nil {
		if mode == types.FeishuUserOAuth && c.PersonalVersion != "" {
			return c.PersonalVersion
		}
		if mode == types.FeishuApplicationGrant && c.ApplicationVersion != "" {
			return c.ApplicationVersion
		}
	}
	return c.Version
}

func (c *FeishuAppConfig) modeConfigured(mode string) bool {
	if c.Capabilities == nil {
		return types.FeishuAuthMode(c.AuthMode) == mode
	}
	return mode == types.FeishuUserOAuth && c.Capabilities.PersonalEnabled ||
		mode == types.FeishuApplicationGrant && c.Capabilities.ApplicationEnabled
}

// V2 is opt-in at both the server and encrypted application configuration.
// Nil capabilities retain the exact legacy single-mode interpretation.
func (c *FeishuAppConfig) modeEnabled(mode string) bool {
	if c == nil || !c.Enabled || mode != types.FeishuUserOAuth && mode != types.FeishuApplicationGrant {
		return false
	}
	if mode == types.FeishuUserOAuth && feishuNonLoopbackHTTP(c.RedirectURI) &&
		!feishuTestHTTPCallbackAllowed(c) && !feishuInternalHTTPCallbackAllowed(c) {
		return false
	}
	if c.Capabilities == nil {
		return types.FeishuAuthMode(c.AuthMode) == mode
	}
	p := *c.Capabilities
	p.RolloutEnabled = os.Getenv("FEISHU_ACCESS_V2_ENABLED") == "1"
	_, err := p.ResolveMode(mode)
	return err == nil
}

func (c *FeishuAppConfig) importMode(requested string) (string, error) {
	if requested == "" {
		if c.Capabilities != nil {
			return "", types.ErrFeishuIdentityRequired
		}
		requested = types.FeishuAuthMode(c.AuthMode)
	}
	if !c.modeEnabled(requested) {
		return "", types.ErrFeishuIdentityDisabled
	}
	return requested, nil
}

// ImportWithIdentity is the V2 admission adapter. It never falls back to another
// identity after a token/ACL error. Legacy callers keep using Import.
func (s *FeishuPersonalService) ImportWithIdentity(
	ctx context.Context,
	kb, link, requested string,
	expected ...types.FeishuCredentialRef,
) (*types.FeishuPersonalImport, error) {
	cfg, err := s.app(ctx)
	if err != nil {
		return nil, err
	}
	mode, err := cfg.importMode(requested)
	if err != nil {
		return nil, err
	}
	if s.recovery == nil {
		if cfg.Capabilities != nil || mode != types.FeishuUserOAuth {
			return nil, errors.New("feishu_safe_pipeline_required")
		}
		return s.Import(ctx, kb, link)
	}
	return s.recovery.prepareWithIdentity(ctx, kb, link, mode, expected...)
}
