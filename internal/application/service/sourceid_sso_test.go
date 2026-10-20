package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/testutil/privatefile"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func sourceIDSSOFixture(
	t *testing.T,
) (*userService, *gorm.DB, *types.OIDCUserInfo, context.Context, string) {
	s, db, info := sourceIDLocalFixture(t)
	c := s.config.OIDCAuth
	c.SourceIDAdmissionMode = "oidc_sso_approved"
	c.SourceIDSSOPolicyConfirmed = true
	c.SourceIDLocalRefreshEnable = false
	c.SourceIDLocalAccessTTL = "60m"
	c.SourceIDLocalAbsoluteTTL = "60m"
	c.SourceIDLocalIdleTTL = "30m"
	c.SourceIDAdmissionMaxAge = "60m"
	c.SourceIDPilotAccountsFile = filepath.Join(t.TempDir(), "ownership.json")
	var user types.User
	require.NoError(t, db.First(&user).Error)
	data, _ := json.Marshal(
		map[string]interface{}{
			"version":       1,
			"policy_ref":    c.SourceIDAutoBindingPolicyRef,
			"ownership_ref": "synthetic-account-history",
			"issuer":        c.IssuerURL,
			"accounts": []map[string]string{
				{"user_id": user.ID, "email": user.Email, "employee_no": "synthetic-001"},
			},
		},
	)
	require.NoError(t, privatefile.WriteFile(c.SourceIDPilotAccountsFile, data, 0o600))
	delete(info.Claims, "mock_eligibility")
	ctx := context.WithValue(
		context.Background(),
		sourceIDAuthorizationProofKey{},
		sourceIDAuthorizationProof{Issuer: c.IssuerURL, Subject: info.Subject, At: time.Now()},
	)
	return s, db, info, ctx, user.ID
}

func TestSourceIDSSOAllRolesAccessOnlyPreservesPermissions(t *testing.T) {
	for _, role := range []string{
		"viewer",
		"contributor",
		"owner",
		"tenant_admin",
		"org_admin",
		"system_admin",
		"cross_space_admin",
		"combined_admin",
	} {
		t.Run(role, func(t *testing.T) {
			s, db, info, ctx, id := sourceIDSSOFixture(t)
			switch role {
			case "viewer", "contributor", "owner":
				require.NoError(t, db.Exec("UPDATE tenant_members SET role=?", role).Error)
			case "tenant_admin":
				require.NoError(t, db.Exec("UPDATE tenant_members SET role='admin'").Error)
			case "org_admin":
				require.NoError(
					t,
					db.Exec("INSERT INTO organization_tenant_members VALUES (42,'admin',?)", id).Error,
				)
			case "system_admin":
				require.NoError(
					t,
					db.Model(&types.User{}).Where("id=?", id).Update("is_system_admin", true).Error,
				)
			case "cross_space_admin":
				require.NoError(
					t,
					db.Model(&types.User{}).Where("id=?", id).Update("can_access_all_tenants", true).Error,
				)
			case "combined_admin":
				require.NoError(
					t,
					db.Model(&types.User{}).
						Where("id=?", id).
						Updates(map[string]interface{}{"is_system_admin": true, "can_access_all_tenants": true}).
						Error,
				)
				require.NoError(t, db.Exec("UPDATE tenant_members SET role='owner'").Error)
			}
			var before types.User
			require.NoError(t, db.First(&before, "id=?", id).Error)
			user, loginCtx, err := s.loginSourceID(ctx, s.config.OIDCAuth, info)
			require.NoError(t, err)
			require.Equal(t, before, *user)
			access, refresh, err := s.generateTokensForTenant(loginCtx, user, 42)
			require.NoError(t, err)
			require.Empty(t, refresh)
			record, err := s.tokenRepo.GetTokenByValue(ctx, access)
			require.NoError(t, err)
			require.WithinDuration(t, time.Now().Add(time.Hour), record.ExpiresAt, 2*time.Second)
			s.tenantService = sourceIDLocalTestTenants{}
			s.memberService = sourceIDLocalTestMembers{userID: id}
			var session types.SourceIDSession
			require.NoError(t, db.First(&session).Error)
			switchCtx, err := s.SourceIDSwitchContext(ctx, access)
			require.NoError(t, err)
			next, err := s.SwitchTenant(switchCtx, user, 42, "")
			require.NoError(t, err)
			require.Empty(t, next.RefreshToken)
			var after types.SourceIDSession
			require.NoError(t, db.First(&after).Error)
			require.Equal(t, session.ExpiresAt, after.ExpiresAt)
			_, _, err = s.ValidateToken(ctx, next.Token)
			require.NoError(t, err)
			var count int64
			require.NoError(
				t,
				db.Model(&types.AuthToken{}).Where("token_type='refresh_token'").Count(&count).Error,
			)
			require.Zero(t, count)
			info.Claims["attributes"].(map[string]interface{})["RJEMAIL"] = "changed@example.test"
			again, _, err := s.loginSourceID(ctx, s.config.OIDCAuth, info)
			require.NoError(t, err)
			require.Equal(t, id, again.ID)
			require.NoError(t, db.Model(&types.User{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
			require.NoError(t, s.Logout(ctx, access))
			_, _, err = s.ValidateToken(ctx, next.Token)
			require.Error(t, err)
		})
	}
}

func TestSourceIDSSOProofScopeAndLifetimeFailClosed(t *testing.T) {
	for _, scenario := range []string{
		"no_verified_transaction",
		"scope",
		"ownership",
		"employee_mismatch",
		"external_mode",
		"idle",
		"absolute",
		"binding_revoke",
		"admin_revoke",
		"manifest_removed",
		"refresh",
	} {
		t.Run(scenario, func(t *testing.T) {
			s, db, info, ctx, id := sourceIDSSOFixture(t)
			if scenario == "no_verified_transaction" {
				ctx = context.Background()
			}
			if scenario == "employee_mismatch" {
				info.Claims["attributes"].(map[string]interface{})["RJGH"] = "synthetic-other"
			}
			if scenario == "external_mode" {
				s.config.OIDCAuth.SourceIDSessionMode = "external"
			}
			if scenario == "scope" || scenario == "ownership" {
				uid, email := id, "wrong@example.test"
				if scenario == "scope" {
					uid = "not-this-user"
					email = "ordinary@example.test"
				}
				data, _ := json.Marshal(
					map[string]interface{}{
						"version":       1,
						"policy_ref":    s.config.OIDCAuth.SourceIDAutoBindingPolicyRef,
						"ownership_ref": "synthetic",
						"issuer":        s.config.OIDCAuth.IssuerURL,
						"accounts": []map[string]string{
							{"user_id": uid, "email": email, "employee_no": "synthetic-001"},
						},
					},
				)
				require.NoError(t, os.WriteFile(s.config.OIDCAuth.SourceIDPilotAccountsFile, data, 0o600))
			}
			user, loginCtx, err := s.loginSourceID(ctx, s.config.OIDCAuth, info)
			if scenario == "no_verified_transaction" || scenario == "scope" || scenario == "ownership" ||
				scenario == "employee_mismatch" ||
				scenario == "external_mode" {
				require.Error(t, err)
				for _, table := range []string{
					"external_identities",
					"source_id_binding_reviews",
					"source_id_enrollments",
					"source_id_sessions",
					"source_id_local_sessions",
				} {
					var n int64
					require.NoError(t, db.Table(table).Count(&n).Error)
					require.Zero(t, n)
				}
				return
			}
			require.NoError(t, err)
			access, refresh, err := s.generateTokensForTenant(loginCtx, user, 42)
			require.NoError(t, err)
			require.Empty(t, refresh)
			store, err := s.sourceIDStore()
			require.NoError(t, err)
			sid := loginCtx.Value(sourceIDSessionKey).(string)
			switch scenario {
			case "idle":
				require.NoError(
					t,
					db.Model(&types.SourceIDLocalSession{}).
						Where("id=?", sid).
						Update("last_seen_at", time.Now().Add(-30*time.Minute)).
						Error,
				)
			case "absolute":
				require.NoError(
					t,
					db.Model(&types.SourceIDSession{}).
						Where("id=?", sid).
						Update("expires_at", time.Now()).
						Error,
				)
			case "binding_revoke":
				var row types.SourceIDBindingReview
				require.NoError(t, db.First(&row).Error)
				r := s.userRepo.(interface {
					ReviewSourceIDBinding(context.Context, string, string, string, string) error
				})
				require.NoError(
					t,
					r.ReviewSourceIDBinding(ctx, row.ID, "synthetic", "synthetic-revoke", "revoke"),
				)
			case "admin_revoke":
				require.NoError(t, store.RevokeSourceIDSession(ctx, sid, id))
			case "manifest_removed":
				require.NoError(t, os.Remove(s.config.OIDCAuth.SourceIDPilotAccountsFile))
			case "refresh":
				_, _, err = s.RefreshToken(ctx, access)
				require.Error(t, err)
				return
			}
			_, _, err = s.ValidateToken(ctx, access)
			require.Error(t, err)
		})
	}
}

func TestSourceIDSSODisabledProviderCannotFallBackToGenericOIDC(t *testing.T) {
	s, db, _, ctx, _ := sourceIDSSOFixture(t)
	s.config.OIDCAuth.SourceIDEnable = false
	_, err := s.GetOIDCAuthorizationURL(ctx, "http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback")
	require.ErrorContains(t, err, "sourceid sso requires sourceid identity gate")
	_, err = s.LoginWithOIDC(
		ctx,
		"synthetic-unused-code",
		"http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback",
		types.TenantProvisioningMode("tenantless"),
	)
	require.ErrorContains(t, err, "sourceid sso requires sourceid identity gate")
	for _, table := range []string{"oidc_transactions", "external_identities", "source_id_sessions"} {
		var count int64
		require.NoError(t, db.Table(table).Count(&count).Error)
		require.Zero(t, count)
	}
}
