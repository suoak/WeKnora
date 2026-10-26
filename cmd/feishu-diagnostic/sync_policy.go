package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"gorm.io/gorm"
)

const syncPolicyAuthorization = "user:2026-10-26:automatic-existing-source-publication"

func syncPublicationPolicies(
	ctx context.Context, db *gorm.DB, settings interfaces.SystemSettingRepository, action string,
) error {
	if action != "inspect" && action != "approve" || action == "approve" &&
		os.Getenv("FEISHU_DIAGNOSTIC_SYNC_POLICY_APPROVAL") != syncPolicyAuthorization {
		return errors.New("explicit_two_source_policy_authorization_required")
	}
	var sources []struct{ ID string }
	if err := db.Raw("SELECT ds.id FROM data_sources ds JOIN feishu_personal_imports m ON m.id=ds.id " +
		"JOIN users u ON u.id=m.user_id JOIN knowledge_bases kb ON kb.id=m.kb_id " +
		"WHERE u.username='test' AND kb.name='test001' AND kb.deleted_at IS NULL " +
		"AND m.resource_type IN ('sheet','bitable') AND m.auth_mode='user_oauth'").Scan(&sources).Error; err != nil {
		return errors.New("policy_source_inventory_unavailable")
	}
	selected := make([]string, 0, 2)
	for _, source := range sources {
		if hash := mask(source.ID); hash == "6d1698a6e0d9" || hash == "1dcf458346ba" {
			selected = append(selected, source.ID)
		}
	}
	if len(selected) != 2 {
		return errors.New("approved_source_pair_missing")
	}
	admin := context.WithValue(ctx, types.SystemAdminContextKey, true)
	if action == "approve" {
		if err := db.WithContext(admin).Transaction(func(tx *gorm.DB) error {
			svc := service.NewFeishuPersonalService(tx, settings, nil, nil, nil)
			for _, id := range selected {
				if _, err := svc.ApproveSourceUpdates(admin, id, "isolated-deployment-operator",
					syncPolicyAuthorization, time.Now().Add(24*time.Hour)); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	var receipts []map[string]any
	for _, id := range selected {
		var row types.SystemSetting
		if err := db.Where("key = ?", "integration.feishu.sync_publication."+id).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				receipts = append(receipts, map[string]any{"source_hash": mask(id), "state": "absent"})
				continue
			}
			return errors.New("policy_receipt_unavailable")
		}
		cipher, err := row.AsString()
		if err != nil {
			return err
		}
		plain, err := utils.DecryptStoredSecret(cipher)
		if err != nil {
			return errors.New("policy_receipt_unreadable")
		}
		var policy service.FeishuSyncPublicationPolicy
		if json.Unmarshal([]byte(plain), &policy) != nil {
			return errors.New("policy_receipt_invalid")
		}
		receipts = append(receipts, map[string]any{
			"source_hash": mask(id), "policy_hash": mask(policy.ID), "state": policy.State,
			"created_at": policy.CreatedAt, "expires_at": policy.ExpiresAt, "operator": policy.Operator,
			"authorization_ref": policy.AuthorizationRef, "initial_approval_hash": mask(policy.InitialApprovalID),
			"source_config_hash": policy.SourceConfigHash, "audience_hash": policy.AudienceHash,
		})
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"status": "POLICY_CHECKED", "operation": action, "policies": receipts,
		"knowledge_writes": false, "sync_control_writes": false, "revision_approvals_rewritten": false,
	})
}
