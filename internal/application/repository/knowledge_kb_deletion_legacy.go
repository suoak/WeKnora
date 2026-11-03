package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CleanupLegacyKnowledgeForKBDeletion is a restrictive operation, not an
// ingestion/read capability. It only consumes a previously authorized, audited
// KB deletion. A fresh transaction-local attempt satisfies the existing write
// fence; no previous nonce, source permission or publication receipt is reused.
// External vector engines require their own coordinated deletion and fail closed.
func (r *knowledgeRepository) CleanupLegacyKnowledgeForKBDeletion(
	ctx context.Context, tenant uint64, kb, kid string, engines []types.RetrieverEngineParams,
) (handled bool, err error) {
	if tenant == 0 || kb == "" || kid == "" {
		return false, errors.New("knowledge base deletion identity not verified")
	}
	if r.db.Name() != "postgres" && !r.db.Migrator().HasTable(&types.FeishuLegacyResource{}) {
		// Lite installations without the PostgreSQL source protocol retain
		// ordinary deletion. An enrolled resource still fails closed below.
		return false, nil
	}
	var enrolled int64
	if err = r.db.WithContext(ctx).Table("feishu_legacy_resources").
		Where("knowledge_id=?", kid).Count(&enrolled).Error; err != nil || enrolled == 0 {
		return false, err
	}
	handled = true
	if r.db.Name() != "postgres" || len(engines) == 0 {
		return true, errors.New("protected deletion requires explicit native PostgreSQL engines")
	}
	for _, engine := range engines {
		if engine.RetrieverEngineType != types.PostgresRetrieverEngineType {
			return true, errors.New("protected deletion cannot coordinate external vector engines")
		}
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the deleted parent before any source/resource locks. A live KB,
		// another tenant, or a substituted vector store cannot use this path.
		var parents []struct{ ID string }
		if e := tx.Raw(`SELECT id FROM knowledge_bases WHERE id=? AND tenant_id=?
 AND deleted_at IS NOT NULL AND vector_store_id IS NULL FOR UPDATE`, kb, tenant).Scan(&parents).Error; e != nil {
			return e
		}
		if len(parents) != 1 {
			return errors.New("protected deletion parent not verified")
		}
		var audit types.AuditLog
		if e := tx.Table("audit_logs").Where(
			`tenant_id=? AND target_type='knowledge_base' AND target_id=? AND action=?
 AND outcome=? AND actor_user_id<>''
 AND created_at >= (SELECT deleted_at FROM knowledge_bases WHERE id=target_id)
 AND created_at <= (SELECT deleted_at+interval '5 minutes' FROM knowledge_bases WHERE id=target_id)`,
			tenant, kb, types.AuditActionKBDeleted, types.AuditOutcomeSuccess,
		).Order("id DESC").Take(&audit).Error; e != nil {
			return errors.New("protected deletion authorization audit missing")
		}
		// Serialize against source execution/claim. Source deletion and execution
		// cancellation must have finished; this operation never releases a hold.
		var sources []struct{ ID string }
		if e := tx.Raw(`SELECT id FROM data_sources WHERE knowledge_base_id=? AND tenant_id=?
 ORDER BY id FOR UPDATE`, kb, tenant).Scan(&sources).Error; e != nil {
			return e
		}
		var unsafe int64
		if e := tx.Table("data_sources").Where(
			"knowledge_base_id=? AND (tenant_id<>? OR deleted_at IS NULL OR legacy_access_state<>'revoked')",
			kb, tenant).Count(&unsafe).Error; e != nil {
			return e
		}
		if unsafe != 0 {
			return errors.New("protected deletion source not terminated")
		}
		if e := tx.Table("sync_logs").Where(
			"data_source_id IN (SELECT id FROM data_sources WHERE knowledge_base_id=?) AND execution_claimed=true", kb,
		).Count(&unsafe).Error; e != nil {
			return e
		}
		if unsafe != 0 {
			return errors.New("protected deletion execution still claimed")
		}
		// Metadata only: no GORM Knowledge/Chunk read hooks are suppressed and no
		// body, summary, snapshot or candidate content is materialized here.
		var resources []types.FeishuLegacyResource
		if e := tx.Table("feishu_legacy_resources").Select(
			"id,tenant_id,knowledge_base_id,data_source_id,external_id,knowledge_id,"+
				"generation,state,current_attempt,active_attempt,source_revision",
		).Where("knowledge_id=?", kid).Scan(&resources).Error; e != nil {
			return e
		}
		if len(resources) != 1 {
			return errors.New("protected deletion resource identity ambiguous")
		}
		res := resources[0]
		if res.TenantID != tenant || res.KnowledgeBaseID != kb || res.KnowledgeID != kid {
			return errors.New("protected deletion resource binding mismatch")
		}
		boundSource := false
		for _, source := range sources {
			boundSource = boundSource || source.ID == res.DataSourceID
		}
		if !boundSource {
			return errors.New("protected deletion source binding mismatch")
		}
		identity := fmt.Sprintf("%d:%s:%s:%s", tenant, kb, res.DataSourceID, res.ExternalID)
		for _, key := range []string{identity, "feishu-kid:" + kid} {
			if e := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", key).Error; e != nil {
				return e
			}
		}
		var locked []struct{ ID string }
		if e := tx.Raw(`SELECT id FROM feishu_legacy_resources WHERE id=? AND tenant_id=?
 AND knowledge_base_id=? AND knowledge_id=? AND data_source_id=? AND external_id=?
 AND generation=? AND current_attempt=? AND active_attempt=? AND state=? FOR UPDATE`,
			res.ID, tenant, kb, kid, res.DataSourceID, res.ExternalID,
			res.Generation, res.CurrentAttempt, res.ActiveAttempt, res.State).Scan(&locked).Error; e != nil {
			return e
		}
		if len(locked) != 1 {
			return errors.New("protected deletion resource changed")
		}
		if e := tx.Table("feishu_legacy_attempts").Where(
			"resource_id=? AND status IN ('ready','staging') AND lease_until>clock_timestamp()", res.ID,
		).Count(&unsafe).Error; e != nil {
			return e
		}
		if unsafe != 0 {
			return errors.New("protected deletion publication lease still live")
		}
		// Exact document lock precedes all destructive writes and idempotency.
		var docs []struct {
			ID      string
			Deleted bool
		}
		if e := tx.Raw(`SELECT id,deleted_at IS NOT NULL AS deleted FROM knowledges
 WHERE id=? AND tenant_id=? AND knowledge_base_id=? FOR UPDATE`, kid, tenant, kb).Scan(&docs).Error; e != nil {
			return e
		}
		if len(docs) != 1 {
			return errors.New("protected deletion knowledge binding mismatch")
		}
		if docs[0].Deleted {
			return nil
		}
		// Reject cross-bound derivative rows instead of deleting foreign data.
		if e := tx.Table("chunks").Where("knowledge_id=? AND (tenant_id<>? OR knowledge_base_id<>?)", kid, tenant, kb).
			Count(&unsafe).Error; e != nil {
			return e
		}
		if unsafe != 0 {
			return errors.New("protected deletion chunk binding mismatch")
		}
		if e := tx.Table("embeddings").Where("knowledge_id=? AND knowledge_base_id<>?", kid, kb).
			Count(&unsafe).Error; e != nil {
			return e
		}
		if unsafe != 0 {
			return errors.New("protected deletion embedding binding mismatch")
		}
		a := types.FeishuLegacyAttempt{
			ID: uuid.NewString(), ResourceID: res.ID, Generation: res.Generation + 1,
			OwnerNonce: uuid.NewString(), Status: "ready", Revision: res.SourceRevision,
			SourceHash: "kb-deletion", Manifest: json.RawMessage(`{}`), Receipt: json.RawMessage(`{}`),
		}
		if e := tx.Raw("SELECT clock_timestamp()+interval '2 minutes'").Scan(&a.LeaseUntil).Error; e != nil {
			return e
		}
		if e := tx.Create(&a).Error; e != nil {
			return e
		}
		changed := tx.Table("feishu_legacy_resources").Where(
			"id=? AND generation=? AND current_attempt=?", res.ID, res.Generation, res.CurrentAttempt,
		).Updates(map[string]any{"generation": a.Generation, "current_attempt": a.ID})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return errors.New("protected deletion generation CAS rejected")
		}
		if e := tx.Exec("SELECT set_config('knowhub.feishu_attempt',?,true),set_config('knowhub.feishu_nonce',?,true)",
			a.ID, a.OwnerNonce).Error; e != nil {
			return e
		}
		for _, statement := range []string{
			"DELETE FROM embeddings WHERE knowledge_id=? AND knowledge_base_id=?",
			"UPDATE chunks SET deleted_at=clock_timestamp(),is_enabled=false " +
				"WHERE knowledge_id=? AND knowledge_base_id=? AND deleted_at IS NULL",
			"UPDATE knowledges SET deleted_at=clock_timestamp() " +
				"WHERE id=? AND knowledge_base_id=? AND deleted_at IS NULL",
		} {
			if e := tx.Exec(statement, kid, kb).Error; e != nil {
				return e
			}
		}
		receipt, _ := json.Marshal(map[string]any{
			"operation": "kb_deletion", "authorization_audit_id": audit.ID, "knowledge_id": kid,
			"prior_generation": res.Generation, "storage_accounting": "reconciliation_required",
		})
		if e := tx.Model(&a).Updates(map[string]any{
			"status": "obsolete", "lease_until": gorm.Expr("clock_timestamp()"), "receipt": json.RawMessage(receipt),
		}).Error; e != nil {
			return e
		}
		// Old workers may already have decremented storage before a fence
		// failure. Never subtract again here without an authoritative ledger.
		return tx.Create(&types.AuditLog{
			TenantID: tenant, ActorUserID: audit.ActorUserID, Action: "kb.protected_cleanup",
			TargetType: "knowledge_base", TargetID: kb, Outcome: types.AuditOutcomeSuccess,
			Details: types.JSON(receipt),
		}).Error
	})
	return handled, err
}
