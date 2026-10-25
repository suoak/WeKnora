package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FeishuBatchView combines durable admission state with verified import progress.
type FeishuBatchView struct {
	Batch  types.FeishuBatch       `json:"batch"`
	Items  []types.FeishuBatchItem `json:"items"`
	Counts map[string]int          `json:"counts"`
	Status string                  `json:"status"`
}

// CreateBatch admits a bounded, owned batch using a caller-supplied idempotency key.
func (s *FeishuPersonalService) CreateBatch(
	ctx context.Context,
	kb, mode, id, key string,
	links []string,
) (*FeishuBatchView, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, access.ErrUnauthorized
	}
	if len(links) < 1 || len(links) > 100 || len(key) < 1 || len(key) > 128 {
		return nil, errors.New("invalid_batch_limits")
	}
	base, err := s.kbs.GetKnowledgeBaseByID(ctx, kb)
	if err != nil {
		return nil, err
	}
	if err = access.RequireKBWrite(ctx, base); err != nil {
		return nil, err
	}
	if s.recovery == nil || !s.recovery.firstCreateOnly || s.db.Name() != "postgres" {
		return nil, errors.New("feishu_safe_pipeline_required")
	}
	_, _, ref, err := s.ResolveCredentialReference(ctx, mode, id)
	if err != nil {
		return nil, err
	}
	unique := []string{}
	seen := map[string]bool{}
	for _, link := range links {
		if len(link) > 4096 {
			return nil, errors.New("invalid_link_length")
		}
		r, e := core.ParsePersonalResourceURL(link)
		if e != nil {
			return nil, e
		}
		if !seen[r.URL] {
			unique = append(unique, r.URL)
			seen[r.URL] = true
		}
	}
	payload, _ := json.Marshal([]any{kb, mode, ref.ConnectionID, unique})
	b := types.FeishuBatch{
		ID:             uuid.NewString(),
		OwnerID:        user,
		TenantID:       types.CallerFromContext(ctx).TenantID,
		KBID:           kb,
		AuthMode:       mode,
		ConnectionID:   ref.ConnectionID,
		ConfigVersion:  ref.ConfigVersion,
		IdempotencyKey: key,
		PayloadHash:    connectionID(string(payload)),
		CreatedAt:      time.Now(),
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&b)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			var existing types.FeishuBatch
			if e := tx.
				Where("owner_id = ? AND kb_id = ? AND idempotency_key = ?",
					user,
					kb,
					key).
				First(&existing).Error; e != nil {
				return e
			}
			if existing.PayloadHash != b.PayloadHash {
				return errors.New("feishu_idempotency_conflict")
			}
			b = existing
			return nil
		}
		for i, link := range unique {
			row := types.FeishuBatchItem{
				ID:        uuid.NewString(),
				BatchID:   b.ID,
				Ordinal:   i,
				URL:       link,
				Status:    "pending",
				UpdatedAt: time.Now(),
			}
			if e := tx.Create(&row).Error; e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Batch(ctx, b.ID)
}

// Batch returns only the authenticated owner's progress after checking KB write access.
func (s *FeishuPersonalService) Batch(ctx context.Context, id string) (*FeishuBatchView, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, access.ErrUnauthorized
	}
	view := &FeishuBatchView{Counts: map[string]int{}, Items: []types.FeishuBatchItem{}}
	if err := s.db.WithContext(ctx).Where("id = ? AND owner_id = ?", id, user).First(&view.Batch).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, access.ErrNotFound
		}
		return nil, err
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, view.Batch.KBID)
	if err != nil {
		return nil, err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return nil, err
	}
	if err = s.db.
		WithContext(ctx).
		Where("batch_id = ?",
			id).
		Order("ordinal").
		Limit(100).
		Find(&view.Items).Error; err != nil {
		return nil, err
	}
	byImport := map[string]types.FeishuPersonalImport{}
	ids := []string{}
	for _, item := range view.Items {
		if item.ImportID != "" {
			ids = append(ids, item.ImportID)
		}
	}
	if len(ids) > 0 {
		imports := []types.FeishuPersonalImport{}
		e := s.db.WithContext(ctx).
			Where("id IN ? AND user_id = ? AND kb_id = ?", ids, user, view.Batch.KBID).
			Limit(100).
			Find(&imports).
			Error
		imports, e = s.decorateFeishuImports(ctx, imports, e)
		if e != nil {
			return nil, e
		}
		for _, m := range imports {
			byImport[m.ID] = m
		}
	}

	for i := range view.Items {
		item := &view.Items[i]
		if item.Status == "dispatched" && item.ImportID != "" {
			for _, m := range byImport {
				if m.ID == item.ImportID {
					switch m.Status {
					case "synced", "completed", "source_managed":
						item.Status = "succeeded"
					case "needs_attention", "attention":
						item.Status = "failed"
						item.ErrorCategory = "review_required"
					}
				}
			}
		}
		view.Counts[item.Status]++
	}
	view.Status = "running"
	if view.Counts["pending"]+view.Counts["processing"]+view.Counts["dispatched"] == 0 {
		view.Status = "succeeded"
		if view.Counts["failed"]+view.Counts["blocked"] > 0 {
			view.Status = "partial_failed"
		}
	}
	return view, nil
}

// RetryBatch explicitly retries bounded failures without resetting source state.
func (s *FeishuPersonalService) RetryBatch(ctx context.Context, id string) error {
	view, err := s.Batch(ctx, id)
	if err != nil {
		return err
	}
	_, _, _, err = s.ResolveCredentialReference(ctx, view.Batch.AuthMode, view.Batch.ConnectionID)
	if err != nil {
		return err
	}
	// Explicit retry re-enters live preflight; it never resets a source cursor,
	// attempts, fences, or a partially published document requiring review.
	return s.db.WithContext(ctx).
		Model(&types.FeishuBatchItem{}).
		Where("batch_id = ? AND status IN ? AND retry_count < 3 AND import_id = ?",
			id,
			[]string{
				"failed",
				"blocked",
			},
			"").
		Updates(map[string]any{
			"status":         "pending",
			"error_category": "",
			"retry_count":    gorm.Expr("retry_count + 1"),
			"updated_at":     time.Now(),
		}).
		Error
}

func (s *FeishuPersonalService) batchContext(
	ctx context.Context,
	b *types.FeishuBatch,
) (context.Context, error) {
	var user types.User
	var member types.TenantMember
	var tenant types.Tenant
	if err := s.db.WithContext(ctx).Where("id = ? AND is_active = ?", b.OwnerID, true).First(&user).Error; err != nil {
		return ctx, access.ErrForbidden
	}
	if err := s.db.
		WithContext(ctx).
		Where("id = ? AND status = ?",
			b.TenantID,
			"active").
		First(&tenant).Error; err != nil {
		return ctx, access.ErrForbidden
	}
	if err := s.db.
		WithContext(ctx).
		Where("user_id = ? AND tenant_id = ? AND status = ?",
			b.OwnerID,
			b.TenantID,
			types.TenantMemberStatusActive).
		First(&member).Error; err != nil {
		return ctx, access.ErrForbidden
	}
	if !member.Role.HasPermission(types.TenantRoleContributor) {
		return ctx, access.ErrForbidden
	}
	fresh := context.WithValue(ctx, types.KBGrantsContextKey, nil)
	fresh = types.WithCaller(fresh, types.Caller{TenantID: b.TenantID, UserID: b.OwnerID, Role: member.Role})
	fresh = context.WithValue(fresh, types.UserIDContextKey, b.OwnerID)
	fresh = context.WithValue(fresh, types.SystemAdminContextKey, user.IsSystemAdmin)
	kb, err := s.kbs.GetKnowledgeBaseByID(fresh, b.KBID)
	if err != nil {
		return ctx, err
	}
	grant, err := access.ResolveKB(
		fresh,
		access.KBRequest{Caller: types.CallerFromContext(fresh)},
		kb,
		types.OrgRoleEditor,
		s.recovery.shares,
		nil,
	)
	if err != nil {
		return ctx, err
	}
	return grant.Context(fresh), nil
}

// ReconcileBatches is called by the existing durable dispatch reconciler. Each
// pass admits at most two items; the existing prepare/outbox owns ingestion.
func (s *FeishuPersonalService) ReconcileBatches(ctx context.Context) error {
	if os.Getenv("FEISHU_ACCESS_V2_ENABLED") != "1" || s.recovery == nil {
		return nil
	}
	if !s.db.Migrator().HasTable(&types.FeishuBatch{}) {
		return errors.New("feishu_v2_migration_required")
	}
	for n := 0; n < 2; n++ {
		var item types.FeishuBatchItem
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if e := tx.
				Clauses(clause.Locking{
					Strength: "UPDATE",
					Options:  "SKIP LOCKED",
				}).
				Where("status = ? OR (status = ? AND lease_until < ?)",
					"pending",
					"processing",
					time.Now()).
				Order("updated_at,id").
				First(&item).Error; e != nil {
				return e
			}
			nonce := uuid.NewString()
			until := time.Now().Add(3 * time.Minute)
			item.Status, item.LeaseOwner, item.LeaseUntil = "processing", nonce, &until
			return tx.Save(&item).Error
		})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var b types.FeishuBatch
		err = s.db.WithContext(ctx).Where("id = ?", item.BatchID).First(&b).Error
		var fresh context.Context
		if err == nil {
			fresh, err = s.batchContext(ctx, &b)
		}
		if err == nil {
			var cfg *FeishuAppConfig
			cfg, err = s.app(fresh)
			if err == nil && cfg.identityVersion(b.AuthMode) != b.ConfigVersion {
				err = ErrFeishuReconnect
			}
		}
		var imported *types.FeishuPersonalImport
		if err == nil {
			_, _, _, err = s.ResolveCredentialReference(fresh, b.AuthMode, b.ConnectionID)
			if err == nil {
				imported, err = s.ImportWithIdentity(fresh, b.KBID, item.URL, b.AuthMode)
			}
		}
		status, category, importID := "dispatched", "", ""
		if err != nil {
			status, category = "blocked", feishuV2Error(err)
			if category == "temporary_failure" {
				status = "failed"
			}
		}
		if imported != nil {
			importID = imported.ID
		}
		if e := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var locked types.FeishuBatch
			if e := tx.
				Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("id = ?",
					b.ID).
				First(&locked).Error; e != nil {
				return e
			}
			if importID != "" {
				var count int64
				if e := tx.
					Model(&types.FeishuBatchItem{}).
					Where("batch_id = ? AND id <> ? AND import_id = ?",
						b.ID,
						item.ID,
						importID).
					Count(&count).Error; e != nil {
					return e
				}
				if count > 0 {
					status = "duplicate"
				}
			}
			return tx.
				Model(&types.FeishuBatchItem{}).
				Where("id = ? AND lease_owner = ? AND lease_until > ?",
					item.ID,
					item.LeaseOwner,
					time.Now()).
				Updates(map[string]any{
					"status":         status,
					"error_category": category,
					"import_id":      importID,
					"lease_owner":    "",
					"lease_until":    nil,
					"updated_at":     time.Now(),
				}).Error
		}); e != nil {
			return e
		}
	}
	return nil
}
