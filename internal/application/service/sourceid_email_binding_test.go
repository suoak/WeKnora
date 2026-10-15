package service

import (
	"context"
	apprepo "github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceIDEmailBindingContractsAndEmployeeStatusFailBeforeRepository(t *testing.T) {
	for _, scenario := range []string{"protocol_only", "identity_contract", "email_contract", "auto_policy_missing", "untrusted_source", "wrong_employee_path", "disabled", "missing_status", "missing_employee", "missing_session"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := &config.OIDCAuthConfig{SourceIDEnable: true, SourceIDEmailBindingEnable: true, SourceIDEmailContractConfirmed: true, SourceIDContractConfirmed: true, SourceIDClaimsSource: "userinfo", SourceIDEmployeeClaim: "attributes.RJGH", SourceIDStatusClaim: "confirmed_status", SourceIDAllowedStatus: "active", SourceIDSessionClaim: "confirmed_session"}
			claims := map[string]interface{}{"attributes": map[string]interface{}{"RJGH": "synthetic-rjgh", "RJEMAIL": "employee@example.test"}, "confirmed_status": "active", "confirmed_session": "synthetic-session"}
			switch scenario {
			case "protocol_only":
				cfg.SourceIDProtocolOnly = true
			case "identity_contract":
				cfg.SourceIDContractConfirmed = false
			case "email_contract":
				cfg.SourceIDEmailContractConfirmed = false
			case "auto_policy_missing":
				cfg.SourceIDAutoBindingEnable = true
			case "untrusted_source":
				cfg.SourceIDClaimsSource = "id_token"
			case "wrong_employee_path":
				cfg.SourceIDEmployeeClaim = "GH"
			case "disabled":
				claims["confirmed_status"] = "disabled"
			case "missing_status":
				delete(claims, "confirmed_status")
			case "missing_employee":
				delete(claims["attributes"].(map[string]interface{}), "RJGH")
			case "missing_session":
				delete(claims, "confirmed_session")
			}
			_, _, err := (&userService{}).loginSourceID(context.Background(), cfg, &types.OIDCUserInfo{Subject: "synthetic-sub", Claims: claims})
			if err == nil || err.Error() == "sourceid repository unavailable" {
				t.Fatal("guard did not reject before repository")
			}
		})
	}
}

func TestSourceIDEmailRJGHApprovedBusinessSessionSynthetic(t *testing.T) {
	for _, mode := range []string{"manual", "auto"} {
		t.Run(mode, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "binding.db")), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			defer sqlDB.Close()
			require.NoError(t, db.AutoMigrate(&types.User{}, &types.AuthToken{}))
			require.NoError(t, db.Exec("DROP INDEX IF EXISTS idx_auth_tokens_session_id").Error)
			require.NoError(t, db.Exec("ALTER TABLE auth_tokens DROP COLUMN session_id").Error)
			for _, path := range []string{"../../../migrations/sqlite/003024_sourceid_oidc.up.sql", "../../../migrations/sqlite/003026_sourceid_binding_review.up.sql"} {
				sql, err := os.ReadFile(path)
				require.NoError(t, err)
				require.NoError(t, db.Exec(string(sql)).Error)
			}
			user := &types.User{ID: uuid.NewString(), Username: "legacy", Email: "employee@example.test", IsActive: true}
			require.NoError(t, db.Create(user).Error)
			repo := apprepo.NewUserRepository(db)
			cfg := &config.OIDCAuthConfig{Enable: true, SourceIDEnable: true, SourceIDEmailBindingEnable: true, SourceIDEmailContractConfirmed: true, SourceIDContractConfirmed: true, SourceIDClaimsSource: "userinfo", IssuerURL: "mock", SourceIDEmployeeClaim: "attributes.RJGH", SourceIDStatusClaim: "confirmed_status", SourceIDAllowedStatus: "active", SourceIDSessionClaim: "confirmed_session"}
			if mode == "auto" {
				cfg.SourceIDAutoBindingEnable = true
				cfg.SourceIDAutoBindingPolicyRef = "approved-synthetic-policy"
				require.NoError(t, db.Exec("CREATE TABLE tenant_members(user_id TEXT,tenant_id BIGINT,role TEXT,status TEXT)").Error)
				require.NoError(t, db.Exec("CREATE TABLE organization_tenant_members(tenant_id BIGINT,role TEXT,representative_user_id TEXT)").Error)
			}
			svc := &userService{config: &config.Config{OIDCAuth: cfg}, userRepo: repo, tokenRepo: apprepo.NewAuthTokenRepository(db)}
			info := &types.OIDCUserInfo{Subject: "synthetic-sub", Claims: map[string]interface{}{"attributes": map[string]interface{}{"RJGH": "0007", "RJEMAIL": "Employee@example.test"}, "confirmed_status": "active", "confirmed_session": "synthetic-session"}}
			ctx := context.Background()
			bound, sessionCtx, err := svc.loginSourceID(ctx, cfg, info)
			if mode == "manual" {
				require.ErrorIs(t, err, apprepo.ErrSourceIDBindingPending)
			} else {
				require.NoError(t, err)
			}
			admin := repo.(interface {
				ListSourceIDBindingReviews(context.Context) ([]types.SourceIDBindingReview, error)
				ReviewSourceIDBinding(context.Context, string, string, string, string) error
			})
			rows, err := admin.ListSourceIDBindingReviews(ctx)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			if mode == "manual" {
				require.NoError(t, admin.ReviewSourceIDBinding(ctx, rows[0].ID, "admin", "synthetic-ownership-proof", "approve"))
				bound, sessionCtx, err = svc.loginSourceID(ctx, cfg, info)
			}
			require.NoError(t, err)
			require.Equal(t, user.ID, bound.ID)
			access, refresh, err := svc.generateTokensForTenant(sessionCtx, bound, 0)
			require.NoError(t, err)
			_, _, err = svc.ValidateToken(ctx, access)
			require.NoError(t, err)
			require.NoError(t, admin.ReviewSourceIDBinding(ctx, rows[0].ID, "admin", "revoke-proof", "revoke"))
			_, _, err = svc.ValidateToken(ctx, access)
			require.Error(t, err)
			_, _, err = svc.RefreshToken(ctx, refresh)
			require.Error(t, err)
		})
	}
}
