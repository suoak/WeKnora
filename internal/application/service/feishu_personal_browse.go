package service

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
)

// Browse lists only the authenticated user's authorized Drive page. It makes
// no KB writes and cannot use a tenant token or an OA/SID credential.
func (s *FeishuPersonalService) Browse(ctx context.Context, folder, page string) (*core.PersonalDrivePage, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, errors.New("user_session_required")
	}
	cfg, err := s.app(ctx)
	if err != nil || !cfg.Enabled {
		return nil, errors.New("feishu_app_unavailable")
	}
	var connection types.FeishuConnection
	if s.db.WithContext(ctx).Where("user_id = ? AND app_id = ?", user, cfg.AppID).First(&connection).Error != nil {
		return nil, ErrFeishuReconnect
	}
	remoteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	remoteCtx = core.WithUserIdentity(remoteCtx, func(callCtx context.Context) (string, error) {
		return s.UserToken(callCtx, user, cfg.AppID, connection.OpenID)
	})
	result, err := s.client(&core.Config{AppID: cfg.AppID}).ListPersonalDrivePage(remoteCtx, folder, page)
	if err != nil {
		return nil, err
	}
	// Disconnect/config rotation during a slow listing must not deliver a stale
	// account's directory. Recheck identity and the current application version.
	current, err := s.app(ctx)
	if err != nil || !current.Enabled || current.Version != cfg.Version {
		return nil, ErrFeishuReconnect
	}
	if _, err := s.UserToken(ctx, user, cfg.AppID, connection.OpenID); err != nil {
		return nil, err
	}
	return result, nil
}
