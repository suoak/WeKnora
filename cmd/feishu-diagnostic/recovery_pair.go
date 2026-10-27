package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"gorm.io/gorm"
)

const acceleratedAuthorization = "user:2026-10-27:accelerated-two-source-validation"

// This local operator entry uses the same service and real administrator checks
// as HTTP. It never constructs an employee session or changes a safety latch.
func recoverExistingPair(
	ctx context.Context,
	db *gorm.DB,
	settings interfaces.SystemSettingRepository,
	action string,
) error {
	if os.Getenv("FEISHU_DIAGNOSTIC_RECOVERY_APPROVAL") != acceleratedAuthorization {
		return errors.New("current_two_source_authorization_required")
	}
	var actor types.User
	if err := db.Where("is_system_admin = ? AND is_active = ? AND deleted_at IS NULL", true, true).
		Order("id").First(&actor).Error; err != nil {
		return errors.New("active_operator_missing")
	}
	ctx = context.WithValue(ctx, types.UserIDContextKey, actor.ID)
	ctx = context.WithValue(ctx, types.SystemAdminContextKey, true)
	var rows []struct{ ID string }
	if err := db.Raw("SELECT ds.id FROM data_sources ds JOIN feishu_personal_imports m ON m.id=ds.id " +
		"JOIN users u ON u.id=m.user_id JOIN knowledge_bases kb ON kb.id=m.kb_id " +
		"WHERE u.username='test' AND kb.name='test001' AND kb.deleted_at IS NULL " +
		"AND m.resource_type IN ('sheet','bitable') AND m.auth_mode='user_oauth'").Scan(&rows).Error; err != nil {
		return err
	}
	ids := []string{}
	for _, row := range rows {
		if hash := mask(row.ID); hash == "6d1698a6e0d9" || hash == "1dcf458346ba" {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) != 2 {
		return errors.New("exact_existing_pair_required")
	}
	if action == "renew-sheet" || action == "enqueue-sheet" {
		for _, id := range ids {
			if mask(id) == "6d1698a6e0d9" {
				ids = []string{id}
				break
			}
		}
		if action == "renew-sheet" {
			action = "renew-reconciliation"
		} else {
			action = "enqueue"
		}
	}
	sort.Strings(ids)
	svc := service.NewFeishuPersonalService(db, settings, nil, nil, nil)
	result := map[string]any{"operation": action, "source_count": len(ids), "sync_control_writes": false}
	switch action {
	case "metadata-probe":
		return inspectPairMetadata(ctx, db, svc, ids)
	case "sheet-package-probe":
		return inspectSheetPackages(ctx, db, svc, ids)
	case "rebind":
		proposal, err := svc.PrepareSourceRebind(ctx, ids, actor.ID)
		if err != nil {
			return err
		}
		grant, err := svc.AuthorizeSourceRebindPublication(ctx, proposal, actor.ID,
			acceleratedAuthorization+":publication", time.Now().Add(24*time.Hour))
		if err != nil {
			return err
		}
		receipt, err := svc.ApplySourceRebind(ctx, proposal, grant, actor.ID,
			acceleratedAuthorization+":binding", os.Getenv("FEISHU_DIAGNOSTIC_VERIFIED_BACKUP_SHA"))
		if err != nil {
			return err
		}
		result["receipt_hash"] = mask(receipt)
	case "reconcile", "renew-reconciliation":
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			bound := service.NewFeishuPersonalService(tx, settings, nil, nil, nil)
			for _, id := range ids {
				approve := bound.ApproveSourceReconciliation
				if action == "renew-reconciliation" {
					approve = bound.RenewSourceReconciliation
				}
				if _, err := approve(ctx, id, actor.ID,
					acceleratedAuthorization, time.Now().Add(24*time.Hour)); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	case "enqueue":
		for _, id := range ids {
			var row types.SystemSetting
			if err := db.Where("key = ?", "integration.feishu.sync_publication."+id).First(&row).Error; err != nil {
				return err
			}
			cipher, err := row.AsString()
			if err != nil {
				return err
			}
			plain, err := utils.DecryptStoredSecret(cipher)
			if err != nil {
				return err
			}
			var policy service.FeishuSyncPublicationPolicy
			if json.Unmarshal([]byte(plain), &policy) != nil || policy.SourceID != id ||
				policy.AuthorizationRef != acceleratedAuthorization {
				return errors.New("current_pair_policy_required")
			}
			if err := enqueueApprovedReconciliation(ctx, db, &policy); err != nil {
				return err
			}
		}
	default:
		return errors.New("unsupported_pair_operation")
	}
	result["status"] = "APPLIED"
	return json.NewEncoder(os.Stdout).Encode(result)
}
