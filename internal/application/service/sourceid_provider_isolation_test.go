package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSourceIDProviderExpiryPreservesBoundedSessionAndDeniesNewLogin(t *testing.T) {
	f := newSIDTLSFixture(t)
	ctx, _ := f.begin(t, "approved-before-expiry")
	response, err := f.s.LoginWithOIDC(ctx, "approved-before-expiry",
		f.s.config.OIDCAuth.RedirectURI, types.TenantProvisioningTenantless)
	require.NoError(t, err)
	require.True(t, response.Success)
	require.Empty(t, response.RefreshToken)
	require.False(t, response.User.IsSystemAdmin)

	pending, _ := f.begin(t, "started-before-expiry")
	f.s.config.OIDCAuth.SourceIDTLSUserInfoExpiresAt = time.Now().Add(-time.Minute).Format(time.RFC3339)
	tokenCalls, profileCalls := f.tokenCalls.Load(), f.profileCalls.Load()
	require.Error(t, config.ValidateConfig(f.s.config))
	require.NoError(t, config.ValidateApplicationConfig(f.s.config))
	_, err = f.s.GetOIDCAuthorizationURL(context.Background(), f.s.config.OIDCAuth.RedirectURI)
	require.ErrorIs(t, err, config.ErrOIDCProviderUnavailable)
	_, err = f.s.LoginWithOIDC(pending, "started-before-expiry",
		f.s.config.OIDCAuth.RedirectURI, types.TenantProvisioningTenantless)
	require.ErrorIs(t, err, config.ErrOIDCProviderUnavailable)
	require.Equal(t, tokenCalls, f.tokenCalls.Load(), "expired policy must not exchange a code")
	require.Equal(t, profileCalls, f.profileCalls.Load())

	user, _, err := f.s.ValidateToken(context.Background(), response.Token)
	require.NoError(t, err, "existing session keeps its independent admission and time limits")
	require.Equal(t, response.User.ID, user.ID)
	require.NoError(t, f.s.Logout(context.Background(), response.Token))
	_, _, err = f.s.ValidateToken(context.Background(), response.Token)
	require.Error(t, err, "provider isolation must not bypass session revocation")
}
