package service

import (
	"context"
	"encoding/json"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestFeishuV2CapabilitiesPreserveLegacyAndRequireExplicitIdentity(t *testing.T) {
	t.Setenv("FEISHU_ACCESS_V2_ENABLED", "")
	legacy := &FeishuAppConfig{Enabled: true, AuthMode: types.FeishuApplicationGrant}
	require.True(t, legacy.modeEnabled(types.FeishuApplicationGrant))
	require.False(t, legacy.modeEnabled(types.FeishuUserOAuth))
	mode, err := legacy.importMode("")
	require.NoError(t, err)
	require.Equal(t, types.FeishuApplicationGrant, mode)
	legacy.Capabilities = &types.FeishuAccessCapabilities{ApplicationEnabled: true, PersonalEnabled: true}
	require.False(t, legacy.modeEnabled(types.FeishuApplicationGrant))
	t.Setenv("FEISHU_ACCESS_V2_ENABLED", "1")
	require.True(t, legacy.modeEnabled(types.FeishuApplicationGrant))
	require.True(t, legacy.modeEnabled(types.FeishuUserOAuth))
	_, err = legacy.importMode("")
	require.ErrorIs(t, err, types.ErrFeishuIdentityRequired)
	legacy.Capabilities.PersonalEnabled = false
	_, err = legacy.importMode(types.FeishuUserOAuth)
	require.ErrorIs(t, err, types.ErrFeishuIdentityDisabled)
	require.True(t, legacy.modeEnabled(types.FeishuApplicationGrant))
}

func TestFeishuV2CredentialReferencesAreOwnerBoundAndRevocable(t *testing.T) {
	s, _ := feishuFixture(t)
	t.Setenv("FEISHU_ACCESS_V2_ENABLED", "1")
	cfg, err := s.app(context.Background())
	require.NoError(t, err)
	cfg.Capabilities = &types.FeishuAccessCapabilities{PersonalEnabled: true, ApplicationEnabled: true}
	admin := context.WithValue(personalUser("A"), types.SystemAdminContextKey, true)
	require.NoError(t, s.SaveApp(admin, cfg))
	for _, user := range []string{"A", "B"} {
		cipher, e := sealFeishu(feishuTokens{UserID: user, Access: "synthetic-" + user, Refresh: "synthetic-refresh"})
		require.NoError(t, e)
		require.NoError(t, s.db.Create(&types.FeishuConnection{UserID: user, AppID: cfg.AppID, OpenID: "openid-" + user, Tokens: cipher, ExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(time.Hour)}).Error)
		require.NoError(t, s.db.Create(&types.FeishuPersonalGrantEpoch{UserID: user, Epoch: "epoch-" + user}).Error)
	}
	remote, client, ref, err := s.ResolveCredentialReference(personalUser("A"), types.FeishuUserOAuth, "")
	require.NoError(t, err)
	_, _, _, err = s.ResolveCredentialReference(personalUser("B"), types.FeishuUserOAuth, ref.ConnectionID)
	require.Error(t, err)
	require.NoError(t, s.Disconnect(personalUser("A")))
	_, err = client.ReadResourcePage(remote, "wiki", "", "", "", 1)
	require.ErrorIs(t, err, ErrFeishuReconnect)
	_, _, _, err = s.ResolveCredentialReference(personalUser("B"), types.FeishuUserOAuth, "")
	require.NoError(t, err)
}

func TestFeishuV2EncryptedConfigAndIndependentOAuth(t *testing.T) {
	s, _ := feishuFixture(t)
	t.Setenv("FEISHU_ACCESS_V2_ENABLED", "1")
	cfg, err := s.app(context.Background())
	require.NoError(t, err)
	cfg.AuthMode = types.FeishuApplicationGrant
	cfg.Capabilities = &types.FeishuAccessCapabilities{ApplicationEnabled: true, PersonalEnabled: true}
	admin := context.WithValue(personalUser("A"), types.SystemAdminContextKey, true)
	require.NoError(t, s.SaveApp(admin, cfg))
	authorization, err := s.Authorize(personalUser("A"))
	require.NoError(t, err)
	require.Contains(t, authorization, "state=")
	require.Equal(t, true, s.Status(personalUser("A"))["available"])
	// Old administrator clients cannot accidentally erase the other capability.
	cfg.Capabilities = nil
	require.Error(t, s.SaveApp(admin, cfg))
	// Disable personal OAuth while leaving application access configured.
	cfg, err = s.app(context.Background())
	require.NoError(t, err)
	cfg.Capabilities.PersonalEnabled = false
	require.NoError(t, s.SaveApp(admin, cfg))
	_, err = s.Authorize(personalUser("A"))
	require.Error(t, err)
	// Disconnection must still delete the user's grant after an admin disables OAuth.
	require.NoError(t, s.Disconnect(personalUser("A")))
	data, err := json.Marshal(s.Status(personalUser("B")))
	require.NoError(t, err)
	require.NotContains(t, string(data), "super-secret")
	require.NotContains(t, string(data), "cli_test")
}
