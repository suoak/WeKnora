package service

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SetFeishuIdentityService binds reference resolution to the existing sync engine.
func (s *DataSourceService) SetFeishuIdentityService(personal *FeishuPersonalService) {
	s.feishuIdentity = personal
	personal.dataSources = s
}

func (s *DataSourceService) bindFeishuSource(
	ctx context.Context,
	ds *types.DataSource,
) (context.Context, error) {
	config, err := ds.ParseConfig()
	if err != nil {
		return ctx, err
	}
	ref, err := types.ParseFeishuSourceReference(config)
	if err != nil || ref == nil {
		return ctx, err
	}
	if !safeLegacyIncrementalEnabled() {
		return ctx, ErrFeishuStagingRequired
	}
	if ds.LegacyAccessState == "revoked" {
		return ctx, access.ErrForbidden
	}
	if s.feishuIdentity == nil || s.feishuIdentity.recovery == nil ||
		ds.Type != types.ConnectorTypeFeishu && ds.Type != types.ConnectorTypeFeishuDrive {
		return ctx, types.ErrFeishuIdentityDisabled
	}
	if err := s.feishuIdentity.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return validatePersonalSourceBinding(tx, ref)
	}); err != nil {
		if !errors.Is(err, ErrFeishuSourceRebindRequired) {
			return ctx, err
		}
		if err := s.feishuIdentity.renewSourceConsent(ctx, ds); err != nil {
			return ctx, err
		}
		config, err = ds.ParseConfig()
		if err != nil {
			return ctx, err
		}
		ref, err = types.ParseFeishuSourceReference(config)
		if err != nil || ref == nil {
			return ctx, ErrFeishuSourceRebindRequired
		}
	}
	reconciling := ds.LegacyAccessState == "hold"
	if reconciling {
		// Only the DB-issued run named by a separate administrator grant may
		// inspect a held source. Ordinary sync, import and UI actions stay closed.
		if err := s.feishuIdentity.checkHeldSourceReconciliation(ctx, ds, ref); err != nil {
			return ctx, access.ErrForbidden
		}
	}
	if execution, internal := types.SyncExecutionFromContext(ctx); !internal {
		user, ok := types.UserIDFromContext(ctx)
		if !ok || user != ref.OwnerUserID {
			return ctx, access.ErrForbidden
		}
	} else if execution.DataSourceID != ds.ID || execution.TenantID != ds.TenantID {
		return ctx, access.ErrForbidden
	}
	fresh, err := s.feishuIdentity.batchContext(
		ctx,
		&types.FeishuBatch{OwnerID: ref.OwnerUserID, TenantID: ds.TenantID, KBID: ds.KnowledgeBaseID},
	)
	if err != nil {
		return ctx, err
	}
	remote, client, credential, err := s.feishuIdentity.ResolveCredentialReference(
		fresh,
		ref.Kind,
		ref.ConnectionID,
	)
	if err != nil {
		return ctx, err
	}
	if credential.AppID != ref.AppID || credential.ConfigVersion != ref.IdentityVersion {
		return ctx, ErrFeishuReconnect
	}
	remote = applicationTableContext(remote)
	links, reference, err := core.ReferenceLinks(config)
	if err != nil {
		return ctx, err
	}
	if !reference {
		return ctx, errors.New("single_object_source_required")
	}
	var expected core.PersonalResource
	if reference {
		// One durable source per canonical imported object. Different documents
		// retain independent source health, leases, cursors and deferrals.
		if len(links) != 1 {
			return ctx, errors.New("single_object_source_required")
		}
		expected, err = core.ParsePersonalResourceURL(links[0])
		if err != nil {
			return ctx, err
		}
		expected, err = client.ResolveBoundResource(applicationTableContext(remote), expected)
		if err != nil {
			return ctx, err
		}
		remote = core.WithExpectedLink(remote, expected)
		remote = types.WithFeishuSourceReadGuard(remote, func(check context.Context) error {
			if reconciling {
				if e := s.feishuIdentity.checkHeldSourceReconciliation(check, ds, ref); e != nil {
					return e
				}
				permissions, e := client.InspectPersonalPermissions(check, expected)
				if e != nil {
					return e
				}
				if !permissions.View || !permissions.Copy || !permissions.Export {
					return core.ErrPersonalBlocksPolicyUnverified
				}
			}
			_, e := client.ResolveBoundResource(check, expected)
			return e
		})
	}
	if ref.Kind == types.FeishuUserOAuth && !reference {
		owner, e := types.PrivateKBOwner(s.feishuIdentity.db.WithContext(fresh), ds.KnowledgeBaseID)
		if e != nil {
			return ctx, e
		}
		if owner != ref.OwnerUserID {
			return ctx, ErrFeishuSharingApproval
		}
	}
	guard := func(tx *gorm.DB) error {
		if ref.Kind == types.FeishuUserOAuth {
			if err := s.feishuIdentity.assertConnectionHealth(tx, ref.OwnerUserID); err != nil {
				return err
			}
			if err := validatePersonalSourceBinding(tx, ref); err != nil {
				return sourceConsentFenceError(tx, ds, ref, err)
			}
		}
		if e := checkRebindPublication(tx, ds.ID, ref.ConnectionID); e != nil {
			return e
		}
		var setting types.SystemSetting
		if e := tx.
			Clauses(clause.Locking{Strength: "SHARE"}).
			Where("key = ?",
				feishuAppSetting).
			First(&setting).Error; e != nil {
			return e
		}
		cipher, e := setting.AsString()
		if e != nil {
			return e
		}
		var cfg FeishuAppConfig
		if e = openFeishu(cipher, &cfg); e != nil {
			return e
		}
		if cfg.AppID != ref.AppID || !cfg.modeEnabled(ref.Kind) ||
			cfg.identityVersion(ref.Kind) != ref.IdentityVersion {
			return ErrFeishuReconnect
		}
		var user types.User
		var member types.TenantMember
		var tenant types.Tenant
		var kb types.KnowledgeBase
		if e = tx.
			Clauses(clause.Locking{Strength: "SHARE"}).
			Where("id = ? AND is_active = ?",
				ref.OwnerUserID,
				true).
			First(&user).Error; e != nil {
			return access.ErrForbidden
		}
		if e = tx.
			Clauses(clause.Locking{Strength: "SHARE"}).
			Where("user_id = ? AND tenant_id = ? AND status = ?",
				ref.OwnerUserID,
				ds.TenantID,
				types.TenantMemberStatusActive).
			First(&member).Error; e != nil {
			return access.ErrForbidden
		}
		if !member.Role.HasPermission(types.TenantRoleContributor) {
			return access.ErrForbidden
		}
		if e = tx.
			Clauses(clause.Locking{Strength: "SHARE"}).
			Where("id = ? AND status = ?",
				ds.TenantID,
				"active").
			First(&tenant).Error; e != nil {
			return access.ErrForbidden
		}
		owned := tx.WithContext(
			context.WithValue(tx.Statement.Context, types.UserIDContextKey, ref.OwnerUserID),
		)
		if e = owned.
			Clauses(clause.Locking{Strength: "SHARE"}).
			Where("id = ? AND tenant_id = ?",
				ds.KnowledgeBaseID,
				ds.TenantID).
			First(&kb).Error; e != nil {
			return e
		}
		if ref.Kind == types.FeishuApplicationGrant {
			if !applicationGrantEnabled() || !applicationUserAllowed(user.IsSystemAdmin) ||
				!applicationKBAllowed(ds.KnowledgeBaseID) {
				return ErrFeishuApplicationRestricted
			}
			return nil
		}
		var conn types.FeishuConnection
		var epoch types.FeishuPersonalGrantEpoch
		if e = tx.
			Clauses(clause.Locking{Strength: "SHARE"}).
			Where("user_id = ? AND app_id = ? AND open_id = ?",
				ref.OwnerUserID,
				credential.AppID,
				credential.OpenID).
			First(&conn).Error; e != nil {
			return ErrFeishuReconnect
		}
		if e = tx.
			Clauses(clause.Locking{Strength: "SHARE"}).
			Where("user_id = ? AND epoch = ?",
				ref.OwnerUserID,
				credential.GrantEpoch).
			First(&epoch).Error; e != nil {
			return sourceConsentFenceError(tx, ds, ref, ErrFeishuReconnect)
		}
		if !conn.ExpiresAt.After(time.Now()) && !conn.RefreshExpiresAt.After(time.Now()) {
			return ErrFeishuReconnect
		}
		if reference {
			if reconciling {
				// Even an exact-version approval cannot replace this one-run
				// recovery grant or authorize a different execution to lift hold.
				return checkSourcePublicationPolicy(owned, ds, ref, expected)
			}
			err := s.feishuIdentity.checkSharingApproval(
				owned,
				&cfg,
				&types.FeishuPersonalImport{
					UserID:        ref.OwnerUserID,
					KBID:          ds.KnowledgeBaseID,
					AppID:         ref.AppID,
					AuthMode:      ref.Kind,
					ResourceType:  expected.Type,
					DocumentToken: expected.Token,
				},
				expected.Modified,
			)
			if errors.Is(err, ErrFeishuSharingApproval) {
				return checkSourcePublicationPolicy(owned, ds, ref, expected)
			}
			return err
		}
		return nil
	}
	return types.WithFeishuSourceCommitGuard(remote, guard), nil
}

func checkFeishuSourceOwner(ctx context.Context, ds *types.DataSource) error {
	config, err := ds.ParseConfig()
	if err != nil {
		return err
	}
	ref, err := types.ParseFeishuSourceReference(config)
	if err != nil || ref == nil {
		return err
	}
	user, ok := types.UserIDFromContext(ctx)
	if !ok || user != ref.OwnerUserID {
		return access.ErrForbidden
	}
	return nil
}

// Refresh only from persisted controls after successful reconciliation. A source
// loaded as held at startup has no cron entry; do not require another app restart.
func (s *DataSourceService) refreshSourceSchedule(ctx context.Context, id string) error {
	if s.scheduler == nil || s.dsRepo == nil {
		return nil
	}
	current, err := s.dsRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	return s.scheduler.AddOrUpdate(current)
}
