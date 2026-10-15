package service

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
)

type sourceIDLocalStore interface {
	GetSourceIDLocalSession(context.Context, string, string) (*types.SourceIDLocalSession, error)
	CreateSourceIDLocalSession(context.Context, *types.SourceIDSession, *types.SourceIDLocalSession) error
}

// Only a formally approved UserInfo status contract is currently executable.
// Authorization-time guarantees/query APIs need their own confirmed adapter;
// a successful code exchange alone is not an employee-admission decision.
func sourceIDAdmission(cfg *config.OIDCAuthConfig, info *types.OIDCUserInfo, now time.Time) (time.Time, error) {
	p, err := cfg.SourceIDLocalPolicy()
	if err != nil {
		return time.Time{}, err
	}
	status := sourceIDClaim(info.Claims, cfg.SourceIDStatusClaim)
	if status == "" {
		return time.Time{}, errors.New("sourceid admission unknown")
	}
	if status != cfg.SourceIDAllowedStatus {
		return time.Time{}, errors.New("sourceid admission denied")
	}
	return now.Add(p.AdmissionMaxAge), nil
}

func (s *userService) sourceIDLocalLimits(ctx context.Context, sessionID, userID string) (*types.SourceIDLocalSession, error) {
	store, ok := s.userRepo.(sourceIDLocalStore)
	if !ok {
		return nil, nil
	} // Existing external-only repository implementations.
	local, err := store.GetSourceIDLocalSession(ctx, sessionID, userID)
	if err != nil || local == nil {
		return local, err
	}
	if s.config == nil || s.config.OIDCAuth == nil {
		return nil, errors.New("sourceid local policy unavailable")
	}
	cfg := s.config.OIDCAuth
	if _, err := cfg.SourceIDLocalPolicy(); err != nil {
		return nil, err
	}
	if cfg.SourceIDLocalPolicyRef != local.PolicyRef || cfg.SourceIDAdmissionPolicyRef != local.AdmissionPolicyRef {
		return nil, errors.New("sourceid local policy changed; reauthentication required")
	}
	return local, nil
}

func (s *userService) revokeSourceIDRefreshReplay(ctx context.Context, record *types.AuthToken) error {
	if record == nil || record.SessionID == "" || record.TokenType != "refresh_token" {
		return nil
	}
	local, err := s.sourceIDLocalLimits(ctx, record.SessionID, record.UserID)
	if err != nil {
		return err
	}
	if local == nil {
		return nil
	}
	store, err := s.sourceIDStore()
	if err != nil {
		return err
	}
	return store.RevokeSourceIDSession(ctx, record.SessionID, record.UserID)
}
