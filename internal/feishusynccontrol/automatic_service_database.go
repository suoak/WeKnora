package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const systemServiceSetting = "integration.feishu.automatic_service_v1"

// A durable transaction receipt, not a human authorization or signature. It
// binds the exact retired policy and fenced runtime to the Redis transition.
type systemServiceReceipt struct {
	Version          int    `json:"version"`
	Namespace        string `json:"namespace"`
	Epoch            string `json:"epoch"`
	RuntimeSHA       string `json:"runtime_sha"`
	WorkerValue      string `json:"worker_value"`
	Phase            string `json:"phase"`
	StateJSON        string `json:"state_json"`
	PriorStateJSON   string `json:"prior_state_json"`
	OriginalTrip     string `json:"original_trip"`
	OriginalNormal   string `json:"original_normal"`
	RetiredNormal    string `json:"retired_normal"`
	OriginalRecovery string `json:"original_recovery"`
	OriginalObserve  string `json:"original_observation"`
	GuardAuditID     string `json:"guard_audit_id"`
	ControlAuditID   string `json:"control_audit_id"`
	EvidenceJSON     string `json:"evidence_json"`
	CreatedMS        int64  `json:"created_ms"`
}

func readSystemReceipt(ctx context.Context, db *gorm.DB) (*systemServiceReceipt, error) {
	var setting types.SystemSetting
	err := db.WithContext(ctx).First(&setting, "key=?", systemServiceSetting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var receipt systemServiceReceipt
	if json.Unmarshal(setting.Value, &receipt) != nil || receipt.Version != 1 ||
		(receipt.Phase != "prepared" && receipt.Phase != "active") {
		return nil, ErrNotReady
	}
	state, err := systemState(receipt.StateJSON)
	if err != nil || state.Epoch != receipt.Epoch || state.RuntimeSHA != receipt.RuntimeSHA ||
		state.WorkerValue != receipt.WorkerValue {
		return nil, ErrNotReady
	}
	return &receipt, nil
}

// prepareSystemReceipt serializes against source claims/publication using the
// existing datasource-first lock order. Pending, unclaimed requests are not
// canceled and archived deliveries are not replayed. A split SQL/Redis commit
// remains prepared/closed until the exact receipt is acknowledged.
func prepareSystemReceipt(ctx context.Context, db *gorm.DB, receipt *systemServiceReceipt,
	previous *systemServiceReceipt,
) error {
	if db == nil || db.Name() != "postgres" || receipt == nil || receipt.Phase != "prepared" {
		return ErrNotReady
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sources []struct{ ID string }
		if err := tx.Raw(`SELECT id FROM data_sources
 WHERE type IN ('feishu','feishu_drive','lark','lark_drive') AND deleted_at IS NULL ORDER BY id FOR UPDATE`).
			Scan(&sources).Error; err != nil {
			return err
		}
		var claimed, publishing, triggers int64
		if err := tx.Raw(`SELECT count(*) FROM sync_logs s JOIN data_sources d ON d.id=s.data_source_id
 WHERE d.type IN ('feishu','feishu_drive','lark','lark_drive') AND s.execution_claimed=true`).
			Scan(&claimed).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT count(*) FROM feishu_legacy_attempts a
 JOIN feishu_legacy_resources r ON r.id=a.resource_id
 WHERE a.id=r.current_attempt AND a.generation=r.generation AND a.status IN ('ready','staging')
 AND a.lease_until>now()`).Scan(&publishing).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgenabled='O'
 AND tgname IN ('feishu_legacy_knowledge_fence','feishu_legacy_chunk_fence','feishu_legacy_embedding_fence')`).
			Scan(&triggers).Error; err != nil {
			return err
		}
		if claimed != 0 || publishing != 0 || triggers != 3 {
			return ErrNotReady
		}
		current, err := readSystemReceipt(ctx, tx.Clauses(clause.Locking{Strength: "UPDATE"}))
		if err != nil || (previous == nil && current != nil) ||
			(previous != nil && (current == nil || *previous != *current)) {
			return errors.Join(ErrNotReady, err)
		}
		// Retire the legacy executable grant DURABLY, preserving its complete
		// signed record/history. An older binary must remain closed on rollback;
		// it cannot ignore the new system Canary and resume its wide grant.
		var legacy types.SystemSetting
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&legacy, "key=?", NormalServiceSetting).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if receipt.OriginalNormal != "" {
				return ErrNotReady
			}
		} else if err != nil {
			return err
		} else {
			var stored, observed NormalServiceRecord
			if json.Unmarshal(legacy.Value, &stored) != nil ||
				json.Unmarshal([]byte(receipt.OriginalNormal), &observed) != nil || stored != observed {
				return ErrNotReady
			}
			retired := stored
			retired.Active = false
			if stored.Active {
				if err := StoreNormalServiceRecord(ctx, tx, retired, &stored); err != nil {
					return err
				}
			}
			raw, err := json.Marshal(retired)
			if err != nil {
				return err
			}
			receipt.RetiredNormal = string(raw)
		}
		raw, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		// Immutable per-epoch evidence survives later current-pointer changes.
		journal := types.SystemSetting{
			Key:   systemServiceSetting + ".epoch." + receipt.Epoch,
			Value: types.JSON(raw), ValueType: "string", Category: "integration",
		}
		if err := tx.Create(&journal).Error; err != nil {
			return err
		}
		if previous == nil {
			journal.ID, journal.Key = 0, systemServiceSetting
			return tx.Create(&journal).Error
		}
		return tx.Model(&types.SystemSetting{}).Where("key=?", systemServiceSetting).
			Update("value", types.JSON(raw)).Error
	})
}

func acknowledgeSystemReceipt(ctx context.Context, db *gorm.DB, receipt systemServiceReceipt) error {
	prepared, err := json.Marshal(receipt)
	if err != nil || receipt.Phase != "prepared" {
		return ErrNotReady
	}
	receipt.Phase = "active"
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	changed := db.WithContext(ctx).Model(&types.SystemSetting{}).
		Where("key=? AND value::jsonb=?::jsonb", systemServiceSetting, string(prepared)).
		Update("value", types.JSON(raw))
	if changed.Error != nil {
		return changed.Error
	}
	if changed.RowsAffected != 1 {
		return ErrNotReady
	}
	return nil
}

// Validate the durable epoch on EVERY execution/publication check; a prepared,
// missing, changed or unreadable SQL receipt never becomes Redis-only authority.
func checkSystemReceipt(ctx context.Context, db *gorm.DB, state systemServiceState) error {
	receipt, err := readSystemReceipt(ctx, db)
	if err != nil || receipt == nil || receipt.Phase != "active" ||
		receipt.Epoch != state.Epoch || receipt.RuntimeSHA != state.RuntimeSHA ||
		receipt.WorkerValue != state.WorkerValue {
		return errors.Join(ErrNotReady, err)
	}
	return nil
}

// Canary result is read from the ordinary durable sync receipt. A successful
// source run alone does not prove publication: unfinished/failed current source
// resources, missing chunks or missing vector rows keep the Canary closed.
func systemCanaryOutcome(ctx context.Context, db *gorm.DB, state systemServiceState) (string, error) {
	if state.CanarySourceID == "" || state.CanaryRunID == "" {
		return "pending", nil
	}
	var log types.SyncLog
	if err := db.WithContext(ctx).First(&log, "id=? AND data_source_id=?", state.CanaryRunID, state.CanarySourceID).Error; err != nil {
		return "", err
	}
	if log.ExecutionClaimed || log.Status == types.SyncLogStatusRunning {
		return "pending", nil
	}
	if log.Status != types.SyncLogStatusSuccess {
		return "source_failed", nil
	}
	var count struct{ Total, Pending, Failed int64 }
	if err := db.WithContext(ctx).Raw(`SELECT count(*) AS total,
 count(*) FILTER (WHERE k.parse_status NOT IN ('completed','failed') OR k.pending_subtasks_count>0) AS pending,
 count(*) FILTER (WHERE k.id IS NULL OR k.deleted_at IS NOT NULL OR k.parse_status='failed') AS failed
 FROM feishu_legacy_resources r LEFT JOIN knowledges k ON k.id=r.knowledge_id
 WHERE r.data_source_id=? AND r.state='active'`, state.CanarySourceID).Scan(&count).Error; err != nil {
		return "", err
	}
	if count.Failed > 0 {
		return "source_failed", nil
	}
	if count.Pending > 0 || count.Total == 0 {
		return "pending", nil
	}
	// Exact knowledge/chunk/index reconciliation belongs to the normal
	// publication path; the Canary additionally refuses empty/missing indexes.
	var missing int64
	if err := db.WithContext(ctx).Raw(`SELECT count(*) FROM feishu_legacy_resources r
 JOIN knowledges k ON k.id=r.knowledge_id AND k.deleted_at IS NULL
 WHERE r.data_source_id=? AND r.state='active' AND
 (NOT EXISTS(SELECT 1 FROM chunks c WHERE c.knowledge_id=k.id AND c.deleted_at IS NULL) OR
  NOT EXISTS(SELECT 1 FROM embeddings e WHERE e.knowledge_id=k.id))`, state.CanarySourceID).Scan(&missing).Error; err != nil {
		return "", err
	}
	if missing != 0 {
		return "source_failed", nil
	}
	return "success", nil
}
