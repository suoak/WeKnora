package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Source uncertainty is durable and independent of the ingestion feature flag.
// Only a successful full reconciliation may release it, and document holds
// remain authoritative even then. Ordinary datasource edits cannot write it.
func (s *FeishuLegacyStore) SetSourceUncertain(ctx context.Context, ds *types.DataSource, uncertain bool) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.execution(tx, ctx, ds); err != nil {
			return err
		}
		state := "hold"
		if !uncertain {
			var unresolved int64
			if err := tx.Model(&types.FeishuLegacyResource{}).Where("data_source_id=? AND state IN ('hold','revoked')", ds.ID).Count(&unresolved).Error; err != nil {
				return err
			}
			if unresolved != 0 {
				return types.ErrLegacySourceRestricted
			}
			state = "active"
		}
		return tx.Model(&types.DataSource{}).Where("id=? AND tenant_id=? AND legacy_access_state <> 'revoked'", ds.ID, ds.TenantID).Update("legacy_access_state", state).Error
	})
}

// RestrictSource makes an uncertain disappearance/permission loss unreadable
// without asserting permanent cloud deletion. confirmed is reserved for a
// trusted server-side withdrawal event; never sourced from request headers or
// connector absence alone. The returned bool reports a durable terminal deny.
func (s *FeishuLegacyStore) RestrictSource(ctx context.Context, ds *types.DataSource, external string, confirmed bool) (terminal bool, err error) {
	if external == "" {
		return false, ErrLegacyVersionFence
	}
	// Deny commits before redaction/cleanup. A failed snapshot, trigger or
	// projection write must not roll the access restriction back to readable.
	if err = s.SetSourceUncertain(ctx, ds, true); err != nil {
		return false, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := s.execution(tx, ctx, ds); e != nil {
			return e
		}
		identity := fmt.Sprintf("%d:%s:%s:%s", ds.TenantID, ds.KnowledgeBaseID, ds.ID, external)
		if e := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", identity).Error; e != nil {
			return e
		}
		id := legacyResourceID(ds, external)
		var r types.FeishuLegacyResource
		e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&r, "id=?", id).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			var rows []types.Knowledge
			if e = tx.Where("tenant_id=? AND knowledge_base_id=? AND metadata->>'datasource_id'=? AND metadata->>'external_id'=?", ds.TenantID, ds.KnowledgeBaseID, ds.ID, external).Limit(2).Find(&rows).Error; e != nil {
				return e
			}
			if len(rows) == 0 {
				terminal = true
				return nil
			}
			if len(rows) != 1 {
				return ErrLegacyVersionFence
			}
			k := rows[0]
			r = types.FeishuLegacyResource{ID: id, TenantID: ds.TenantID, KnowledgeBaseID: ds.KnowledgeBaseID, DataSourceID: ds.ID, ExternalID: external, KnowledgeID: k.ID, OriginalFilePath: k.FilePath, State: "active", QuarantineSnapshot: json.RawMessage(`{}`)}
			if e = tx.Create(&r).Error; e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		if r.State == "revoked" {
			terminal = true
			return nil
		}
		if r.State == "hold" && !confirmed && len(r.QuarantineSnapshot) > 2 {
			return nil
		}
		if e = tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", "feishu-kid:"+r.KnowledgeID).Error; e != nil {
			return e
		}
		// Preserve the effective version privately BEFORE any public projection
		// changes. Repeated uncertainty must not replace it with a redacted copy.
		if e = tx.Exec(`UPDATE feishu_legacy_resources SET quarantine_snapshot=jsonb_build_object(
 'knowledge',(SELECT to_jsonb(k) FROM knowledges k WHERE k.id=?),
 'chunks',(SELECT COALESCE(jsonb_agg(to_jsonb(c)),'[]') FROM chunks c WHERE c.knowledge_id=? AND c.deleted_at IS NULL),
 'embeddings',(SELECT COALESCE(jsonb_agg(to_jsonb(v)),'[]') FROM embeddings v WHERE v.knowledge_id=?))
 WHERE id=? AND quarantine_snapshot='{}'::jsonb`, r.KnowledgeID, r.KnowledgeID, r.KnowledgeID, r.ID).Error; e != nil {
			return e
		}
		r.Generation++
		r.CurrentAttempt = uuid.NewString()
		r.State = "hold"
		if confirmed {
			r.State = "revoked"
			terminal = true
		}
		a := types.FeishuLegacyAttempt{ID: r.CurrentAttempt, ResourceID: r.ID, Generation: r.Generation, OwnerNonce: uuid.NewString(), Status: "ready", Revision: r.SourceRevision, SourceHash: "withdrawal", Manifest: json.RawMessage(`{}`), Receipt: json.RawMessage(`{}`)}
		if e = tx.Raw("SELECT clock_timestamp()+interval '2 minutes'").Scan(&a.LeaseUntil).Error; e != nil {
			return e
		}
		if e = tx.Model(&r).Updates(map[string]any{"generation": r.Generation, "current_attempt": r.CurrentAttempt, "state": r.State}).Error; e != nil {
			return e
		}
		if e = tx.Create(&a).Error; e != nil {
			return e
		}
		if e = tx.Exec("SELECT set_config('knowhub.feishu_attempt',?,true),set_config('knowhub.feishu_nonce',?,true)", a.ID, a.OwnerNonce).Error; e != nil {
			return e
		}
		// A persistent logical deny and public redaction commit together. There
		// is NO physical file deletion; private snapshots remain recoverable.
		metadata, _ := json.Marshal(map[string]string{"datasource_id": ds.ID, "external_id": external})
		if e = tx.Model(&types.Knowledge{}).Where("id=? AND tenant_id=?", r.KnowledgeID, r.TenantID).Updates(map[string]any{"enable_status": "disabled", "title": "Source access restricted", "description": "", "profile": nil, "source": "", "file_name": "", "file_path": "", "metadata": types.JSON(metadata), "error_message": "Source access requires verification", "updated_at": time.Now().UTC()}).Error; e != nil {
			return e
		}
		if e = tx.Exec("UPDATE chunks SET content='',source_content='',context_header='',image_info='',source_locators='[]',metadata='{}',is_enabled=false,deleted_at=clock_timestamp() WHERE knowledge_id=? AND tenant_id=? AND deleted_at IS NULL", r.KnowledgeID, r.TenantID).Error; e != nil {
			return e
		}
		if e = tx.Exec("UPDATE embeddings SET content='',is_enabled=false WHERE knowledge_id=? AND knowledge_base_id=?", r.KnowledgeID, r.KnowledgeBaseID).Error; e != nil {
			return e
		}
		return tx.Model(&a).Updates(map[string]any{"status": "published", "receipt": json.RawMessage(`{"restricted":true}`)}).Error
	})
	return
}
