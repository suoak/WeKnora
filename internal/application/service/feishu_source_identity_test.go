package service

import (
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFeishuRemoteFailureSourceBindingSeparatesReauthorizationFromRefresh(t *testing.T) {
	s, _ := feishuFixture(t)
	require.NoError(t, s.db.Create(&types.FeishuConnection{
		UserID: "A", AppID: "cli_test", OpenID: "open-A", Tokens: "unused",
		ExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(24 * time.Hour),
	}).Error)
	require.NoError(t, s.db.Create(&types.FeishuPersonalGrantEpoch{UserID: "A", Epoch: "original"}).Error)
	ref := &types.FeishuSourceReference{
		Version: 1, OwnerUserID: "A", AppID: "cli_test", Kind: types.FeishuUserOAuth,
		ConnectionID: connectionID(types.FeishuUserOAuth, "A", "cli_test", "open-A", "original"),
	}
	check := func() error {
		return s.db.Transaction(func(tx *gorm.DB) error { return validatePersonalSourceBinding(tx, ref) })
	}
	require.NoError(t, check())
	require.NoError(t, s.db.Model(&types.FeishuConnection{}).Where("user_id = ?", "A").
		Updates(map[string]any{"tokens": "refreshed", "expires_at": time.Now().Add(2 * time.Hour)}).Error)
	require.NoError(t, check(), "normal token refresh must not invalidate the source")
	require.NoError(t, s.db.Model(&types.FeishuPersonalGrantEpoch{}).Where("user_id = ?", "A").
		Update("epoch", "supplemental-authorization").Error)
	require.ErrorIs(t, check(), ErrFeishuSourceRebindRequired)
	require.Equal(t, "source_identity_rebind_required", feishuRemoteFailureCategory(check()))
	action, retryable := feishuFailureAction(feishuRemoteFailureCategory(check()))
	require.Equal(t, "contact_admin", action)
	require.False(t, retryable)
	require.Equal(t, connectionID(types.FeishuUserOAuth, "A", "cli_test", "open-A", "original"), ref.ConnectionID,
		"checking a new grant never rewrites a persisted source")
	require.NoError(t, s.db.Where("user_id = ?", "A").Delete(&types.FeishuConnection{}).Error)
	require.ErrorIs(t, check(), ErrFeishuReconnect, "missing authorization is a different failure")
}
