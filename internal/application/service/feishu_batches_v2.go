package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FeishuBatchView combines durable admission state with verified import progress.
type FeishuBatchView struct {
	Batch                  types.FeishuBatch       `json:"batch"`
	Items                  []types.FeishuBatchItem `json:"items"`
	Counts                 map[string]int          `json:"counts"`
	Status                 string                  `json:"status"`
	TargetName             string                  `json:"target_name"`
	CanContinuePublication bool                    `json:"can_continue_publication"`
}

// ListBatches recovers the owner's batch handles after a page or worker restart.
// Progress is read separately so listing history never replays an admission.
func (s *FeishuPersonalService) ListBatches(
	ctx context.Context,
	kbID string,
	offset int,
) ([]types.FeishuBatch, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, access.ErrUnauthorized
	}
	if offset < 0 || offset > 10000 {
		return nil, errors.New("invalid_batch_history_offset")
	}
	if kbID == "" {
		rows := []types.FeishuBatch{}
		err := s.db.WithContext(ctx).
			Where("owner_id = ? AND tenant_id = ?", user, types.CallerFromContext(ctx).TenantID).
			Order("created_at DESC, id DESC").
			Offset(offset).
			Limit(20).
			Find(&rows).
			Error
		if err != nil {
			return nil, err
		}
		return s.batchHistorySummaries(ctx, rows)
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return nil, err
	}
	rows := []types.FeishuBatch{}
	err = s.db.WithContext(ctx).
		Where("owner_id = ? AND kb_id = ? AND tenant_id = ?", user, kbID, kb.TenantID).
		Order("created_at DESC, id DESC").
		Offset(offset).
		Limit(20).
		Find(&rows).
		Error
	if err != nil {
		return nil, err
	}
	return s.batchHistorySummaries(ctx, rows)
}

// Bounded metadata only: no vector counts or parser polling for every history
// row. Titles are returned only after resolving this user's live destination ACL.
func (s *FeishuPersonalService) batchHistorySummaries(
	ctx context.Context,
	rows []types.FeishuBatch,
) ([]types.FeishuBatch, error) {
	result := make([]types.FeishuBatch, 0, len(rows))
	for _, batch := range rows {
		kb, err := s.kbs.GetKnowledgeBaseByID(ctx, batch.KBID)
		if err != nil || s.recovery == nil {
			continue
		}
		grant, err := access.ResolveKB(
			ctx,
			access.KBRequest{Caller: types.CallerFromContext(ctx)},
			kb,
			types.OrgRoleEditor,
			s.recovery.shares,
			nil,
		)
		if err != nil || access.RequireKBWrite(grant.Context(ctx), kb) != nil {
			continue
		}
		var items []struct {
			Title       string
			Status      string
			IndexStatus string
		}
		if err := s.db.WithContext(ctx).Table("feishu_import_batch_items AS bi").
			Select("k.title, bi.status, CASE WHEN k.parse_status = 'completed'"+
				" AND pi.status IN ('synced','completed','source_managed')"+
				" AND EXISTS (SELECT 1 FROM task_pending_ops AS receipt WHERE receipt.scope = 'personal_index_receipt'"+
				" AND receipt.scope_id = pi.knowledge_id AND receipt.op = 'verified')"+
				" THEN 'verified' ELSE '' END AS index_status").
			Joins("LEFT JOIN feishu_personal_imports AS pi ON pi.id = bi.import_id "+
				"AND pi.user_id = ? AND pi.kb_id = ?", batch.OwnerID, batch.KBID).
			Joins("LEFT JOIN knowledges AS k ON k.id = pi.knowledge_id AND "+
				"k.knowledge_base_id = ? AND k.deleted_at IS NULL", batch.KBID).
			Where("bi.batch_id = ?", batch.ID).Order("bi.ordinal").Limit(100).Scan(&items).Error; err != nil {
			return nil, err
		}
		batch.TargetName, batch.Status = kb.Name, "succeeded"
		for _, item := range items {
			if item.Title != "" {
				batch.DocumentNames = append(batch.DocumentNames, item.Title)
			}
			if item.Status == "failed" || item.Status == "blocked" {
				batch.Status = "failed"
			}
			pending := item.Status != "succeeded" && item.Status != "dispatched" && item.Status != "duplicate"
			if batch.Status != "failed" && (pending || item.IndexStatus != "verified") {
				batch.Status = "processing"
			}
		}
		if len(items) == 0 {
			batch.Status = "processing"
		}
		result = append(result, batch)
	}
	return result, nil
}

// CreateBatch admits a bounded, owned batch using a caller-supplied idempotency key.
func (s *FeishuPersonalService) CreateBatch(
	ctx context.Context,
	kb, mode, id, key string,
	links []string,
	publication ...bool,
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
	// Legacy publication arguments are ignored; destination authorization is KB write access.
	if s.recovery == nil || !s.recovery.firstCreateOnly || s.db.Name() != "postgres" {
		return nil, errors.New("feishu_safe_pipeline_required")
	}
	_, _, ref, err := s.ResolveCredentialReference(ctx, mode, id)
	if err != nil {
		return nil, err
	}
	unique := []string{}
	invalid := map[string]bool{}
	seen := map[string]bool{}
	for _, link := range links {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		if len(link) > 4096 {
			return nil, errors.New("invalid_link_length")
		}
		r, e := core.ParsePersonalResourceURL(link)
		if e != nil {
			if !seen[link] {
				unique = append(unique, link)
				seen[link] = true
				invalid[link] = true
			}
			continue
		}
		if !seen[r.URL] {
			unique = append(unique, r.URL)
			seen[r.URL] = true
		}
	}
	if len(unique) == 0 {
		return nil, errors.New("invalid_batch_limits")
	}
	identity := []any{kb, mode, ref.ConnectionID, unique}
	payload, _ := json.Marshal(identity)
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
			if invalid[link] {
				row.Status = "blocked"
				row.ErrorCategory = "invalid_feishu_url"
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
	// Admission performs no remote metadata, download or indexing work.
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
	view.TargetName = kb.Name
	if s.recovery == nil {
		return nil, ErrFeishuStagingRequired
	}
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: types.CallerFromContext(ctx)},
		kb, types.OrgRoleEditor, s.recovery.shares, nil)
	if err != nil {
		return nil, err
	}
	ctx = grant.Context(ctx)
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

	queue := &batchQueueProgress{}
	for i := range view.Items {
		item := &view.Items[i]
		if (item.Status == "dispatched" || item.Status == "duplicate") && item.ImportID != "" {
			// An alias is deduplicated admission, not completed ingestion.
			item.Status = "dispatched"
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
		var current *types.FeishuPersonalImport
		if m, ok := byImport[item.ImportID]; ok {
			value := m
			current = &value
		}
		if err := s.batchItemProgress(ctx, item, current, view.Batch.CreatedAt, queue); err != nil {
			return nil, err
		}
		view.Counts[item.Status]++
		if item.Status == "blocked" && item.ErrorCategory == "sharing_approval_required" &&
			item.ImportID == "" && item.SharingApprovalID == nil && item.RetryCount < 3 &&
			view.Batch.AuthMode == types.FeishuUserOAuth {
			view.CanContinuePublication = true
		}
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
	if err := s.requireImportWriter(s.db.
		WithContext(ctx), view.Batch.OwnerID, view.Batch.KBID, view.Batch.TenantID); err != nil {
		return err
	}
	_, _, _, err = s.ResolveCredentialReference(ctx, view.Batch.AuthMode, view.Batch.ConnectionID)
	if err != nil {
		return err
	}
	// Explicit retry re-enters live preflight; it never resets a source cursor,
	// attempts, fences, or a partially published document requiring review.
	retry := s.db.WithContext(ctx).
		Model(&types.FeishuBatchItem{}).
		Where("batch_id = ? AND status IN ? AND retry_count < 3 AND (import_id = ? OR error_category IN ?)",
			id,
			[]string{
				"failed",
				"blocked",
			},
			"", []string{"sharing_approval_required", "publication_confirmation_expired"})
	if s.db.Migrator().HasTable(&types.DataSource{}) {
		// Retrying an old admission is never a source-resume operation, even if
		// the UI's old error category was stale. Preserve paused/hold/revoked rows.
		retry = retry.Where(
			"NOT EXISTS (SELECT 1 FROM data_sources AS protected_source WHERE " +
				"protected_source.id = feishu_import_batch_items.import_id)",
		)
	}
	return retry.Updates(map[string]any{
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
	fresh = types.WithPrincipal(fresh, types.Principal{Type: types.PrincipalWebUser, ID: b.OwnerID})
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
	if err := s.requeueUnavailableParserBatches(ctx); err != nil {
		return err
	}
	if err := s.reconcileBatchUpdates(ctx); err != nil {
		return err
	}
	for n := 0; n < 2; n++ {
		var item types.FeishuBatchItem
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if e := tx.
				Clauses(clause.Locking{
					Strength: "UPDATE",
					Options:  "SKIP LOCKED",
				}).
				Where("(status = ? AND updated_at <= ?) OR (status = ? AND lease_until < ?)",
					"pending", time.Now(), "processing", time.Now()).
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
			var ref types.FeishuCredentialRef
			_, _, ref, err = s.ResolveCredentialReference(fresh, b.AuthMode, b.ConnectionID)
			if err == nil {
				err = s.bindBatchPublication(fresh, &b, &item)
			}
			if err == nil {
				fresh = batchPublicationContext(fresh, &item)
				imported, err = s.ImportWithIdentity(fresh, b.KBID, item.URL, b.AuthMode, ref)
			}
		}
		status, category, importID := "dispatched", "", item.ImportID
		if err != nil {
			status, category = "blocked", feishuV2Error(err)
			if category == "temporary_failure" {
				status = "failed"
				if item.RetryCount < 3 {
					status = "pending"
				}
			}
		}
		if imported != nil {
			importID = imported.ID
			if imported.Status == "source_managed" {
				status = "existing"
			}
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
					"updated_at":     batchNextAttempt(status, item.RetryCount),
					"retry_count":    batchRetryCount(status, item.RetryCount),
				}).Error
		}); e != nil {
			return e
		}
	}
	return nil
}

// Retry delays are durable; leases and retry counters survive worker restarts.
func batchNextAttempt(status string, retries int) time.Time {
	if status == "pending" {
		return time.Now().Add(time.Duration(1<<retries) * 30 * time.Second)
	}
	return time.Now()
}

func batchRetryCount(status string, retries int) int {
	if status == "pending" {
		return retries + 1
	}
	return retries
}

// Enroll only explicitly imported and verified resources. No directory discovery,
// source resume or cursor reset occurs here. Existing handoff fences remain authoritative.
func (s *FeishuPersonalService) reconcileBatchUpdates(ctx context.Context) error {
	for n := 0; n < 2; n++ {
		var item types.FeishuBatchItem
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
				Where("status = ? AND import_id <> ? AND updated_at <= ? AND (lease_until IS NULL OR lease_until < ?)",
					"dispatched", "", time.Now(), time.Now()).Order("updated_at,id").First(&item).Error; e != nil {
				return e
			}
			until := time.Now().Add(3 * time.Minute)
			item.LeaseOwner, item.LeaseUntil = uuid.NewString(), &until
			return tx.Save(&item).Error
		})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		status, category := "dispatched", ""
		var b types.FeishuBatch
		err = s.db.WithContext(ctx).Where("id = ?", item.BatchID).First(&b).Error
		var fresh context.Context
		if err == nil {
			fresh, err = s.batchContext(ctx, &b)
		}
		var view *FeishuBatchView
		if err == nil {
			view, err = s.Batch(fresh, b.ID)
		}
		if err == nil {
			for _, progress := range view.Items {
				if progress.ID != item.ID {
					continue
				}
				if progress.Stalled {
					logger.Warnf(
						fresh,
						"Feishu import stalled: no live document lease, recent activity or queued task",
					)
				}
				switch progress.Status {
				case "succeeded":
					// Retain exactly the identity/version admitted by the original batch.
					var ref types.FeishuCredentialRef
					_, _, ref, err = s.ResolveCredentialReference(fresh, b.AuthMode, b.ConnectionID)
					if err == nil && ref.ConfigVersion != b.ConfigVersion {
						err = ErrFeishuReconnect
					}
					if err == nil {
						_, err = s.EnableImportSource(fresh, b.KBID, item.ImportID, "")
					}
					if err == nil {
						status = "succeeded"
					}
				case "failed":
					status, category = "failed", progress.ErrorCategory
				}
			}
		}
		retries := item.RetryCount
		if err != nil {
			status, category = "blocked", feishuV2Error(err)
			if category == "temporary_failure" && retries < 3 {
				status = "dispatched"
				retries++
			}
		}
		// A bounded delay gives other documents a turn while indexing/enrollment waits.
		next := time.Now().Add(30 * time.Second)
		if category == "temporary_failure" {
			next = time.Now().Add(time.Duration(1<<retries) * 30 * time.Second)
		}
		change := s.db.WithContext(ctx).Model(&types.FeishuBatchItem{}).
			Where("id = ? AND status = ? AND lease_owner = ? AND lease_until > ?",
				item.ID, "dispatched", item.LeaseOwner, time.Now()).
			Updates(map[string]any{
				"status": status, "error_category": category, "retry_count": retries,
				"updated_at": next, "lease_owner": "", "lease_until": nil,
			})
		if change.Error != nil {
			return change.Error
		}
	}
	return nil
}
