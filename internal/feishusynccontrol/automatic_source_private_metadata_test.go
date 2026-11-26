package feishusynccontrol

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAutomaticSourcePostgresPrivatePolicyMetadataKeepsOwnerGuard(t *testing.T) {
	for _, mode := range []string{
		"owner", "other_private_owner", "suspended_member", "http_principal", "sibling_hold", "source_hold",
	} {
		t.Run(mode, func(t *testing.T) {
			f := newAutomaticAuthorityFixture(t)
			ref := types.FeishuSourceReference{
				Version: 1, OwnerUserID: f.owner.ID, Kind: types.FeishuUserOAuth,
				ConnectionID: "fixture-reference", AppID: "fixture-app", IdentityVersion: "fixture-version",
			}
			config := types.DataSourceConfig{
				ResourceIDs: []string{"fixture-root"},
				Settings:    map[string]interface{}{"credential_reference": ref},
			}
			raw, err := config.ToJSON()
			require.NoError(t, err)
			f.source.Config = raw
			require.NoError(t, f.db.Model(&f.source).Update("config", raw).Error)
			f.refreshConfig(t)
			require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES(?,?)",
				f.source.KnowledgeBaseID, f.owner.ID).Error)
			f.db = types.EnablePrivateKBGuards(f.db)
			f.db = types.EnableLegacyReadGuards(f.db)
			// Payload audit identity never chooses the metadata policy actor.
			ctx := (types.TaskInitiator{UserID: "payload-only", Role: types.TenantRoleOwner}).Apply(
				context.Background(),
			)
			before := types.CallerFromContext(ctx)
			assertPrivateClosed := func() {
				var metadata types.KnowledgeBase
				err := f.db.WithContext(ctx).Select("id,tenant_id,deleted_at").
					Where("id=?", f.source.KnowledgeBaseID).Take(&metadata).Error
				require.ErrorIs(t, err, gorm.ErrRecordNotFound)
			}
			assertPrivateClosed()
			switch mode {
			case "other_private_owner":
				require.NoError(t, f.db.Exec("UPDATE feishu_private_kbs SET owner_id=? WHERE kb_id=?",
					f.viewer.ID, f.source.KnowledgeBaseID).Error)
			case "suspended_member":
				require.NoError(t, f.db.Table("tenant_members").Where("user_id=?", f.owner.ID).
					Update("status", types.TenantMemberStatusSuspended).Error)
			case "http_principal":
				ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: f.owner.ID})
			case "sibling_hold":
				sibling := types.DataSource{
					ID: uuid.NewString(), TenantID: f.tenant.ID, KnowledgeBaseID: f.source.KnowledgeBaseID,
					Type: "feishu", Status: "error", LegacyAccessState: "hold", Config: raw,
				}
				require.NoError(t, f.db.Create(&sibling).Error)
				var bodyRead types.KnowledgeBase
				require.ErrorIs(t, f.db.WithContext(f.userContext(f.owner)).
					Where("id=?", f.source.KnowledgeBaseID).Take(&bodyRead).Error, types.ErrLegacySourceRestricted)
			case "source_hold":
				f.source.LegacyAccessState = "hold"
				require.NoError(t, f.db.Model(&f.source).Update("legacy_access_state", "hold").Error)
				f.refreshConfig(t)
			}
			out, err := automaticBackgroundSourceAuthority(ctx, f.db, nil, f.source.ID, f.config)
			if mode == "owner" || mode == "sibling_hold" {
				require.NoError(t, err)
				require.Equal(t, f.owner.ID, out.ActorID)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, before, types.CallerFromContext(ctx))
			if mode != "http_principal" {
				require.Nil(t, ctx.Value(types.PrincipalContextKey))
				assertPrivateClosed() // No local policy identity escapes into the caller.
			}
		})
	}
}
