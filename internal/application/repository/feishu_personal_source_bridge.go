package repository

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ExactSourceHandoff retains the first publication until the ordinary source
// publisher commits its replacement. It never grants content access.
type ExactSourceHandoff struct {
	imported types.FeishuPersonalImport
	resource types.FeishuPersonalResource
	attempt  types.FeishuPersonalAttempt
	receipt  types.TaskPendingOp
}

func (p *ExactSourceHandoff) KnowledgeID() string { return p.imported.KnowledgeID }

// FindPersonalExactSourceHandoff recognizes only the source whose server-owned
// ID and credential reference match an independently verified first import.
// A missing first-import row is an ordinary source, not an error or a grant.
func FindPersonalExactSourceHandoff(
	db *gorm.DB, ds *types.DataSource, external string, locked bool,
) (*ExactSourceHandoff, error) {
	cfg, err := ds.ParseConfig()
	if err != nil {
		return nil, err
	}
	ref, err := types.ParseFeishuSourceReference(cfg)
	if err != nil || ref == nil {
		return nil, err
	}
	query := func() *gorm.DB {
		q := db.Session(&gorm.Session{NewDB: true}).WithContext(db.Statement.Context)
		if locked {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		return q
	}
	reader := db.Session(&gorm.Session{NewDB: true}).WithContext(db.Statement.Context)
	var imported types.FeishuPersonalImport
	err = reader.Where("id=? AND kb_id=?", ds.ID, ds.KnowledgeBaseID).Take(&imported).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var receipts []types.TaskPendingOp
	if err = reader.Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope,
		imported.KnowledgeID).Limit(2).Find(&receipts).Error; err != nil {
		return nil, err
	}
	if len(receipts) == 0 {
		if imported.Status != "source_managed" {
			return nil, legacyFence("personal_handoff_receipt_missing")
		}
		return nil, nil
	}
	// Match first-create lock ordering: resource, attempt, import, receipt.
	p := &ExactSourceHandoff{}
	if err = query().Where("import_id=?", imported.ID).Take(&p.resource).Error; err != nil {
		return nil, err
	}
	if err = query().Where("id=?", p.resource.CurrentAttemptID).Take(&p.attempt).Error; err != nil {
		return nil, err
	}
	if locked {
		if err = query().Where("id=? AND kb_id=?", ds.ID, ds.KnowledgeBaseID).Take(&imported).Error; err != nil {
			return nil, err
		}
		receipts = nil
		if err = query().Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope,
			imported.KnowledgeID).Limit(2).Find(&receipts).Error; err != nil {
			return nil, err
		}
	}
	if len(receipts) != 1 || receipts[0].Op != "published" || receipts[0].TenantID != ds.TenantID ||
		imported.Status != "indexed" || imported.KnowledgeID == "" ||
		external != imported.ResourceType+":"+imported.DocumentToken {
		return nil, legacyFence("personal_handoff_unconfirmed")
	}
	if ref.OwnerUserID != imported.UserID ||
		ref.AppID != imported.AppID || ref.Kind != imported.AuthMode {
		return nil, legacyFence("personal_handoff_identity_changed")
	}
	var receipt types.PersonalExactReceipt
	if json.Unmarshal(receipts[0].Payload, &receipt) != nil || receipt.Version != 1 ||
		receipt.ImportID != imported.ID || receipt.KnowledgeID != imported.KnowledgeID ||
		receipt.KnowledgeBaseID != ds.KnowledgeBaseID || receipt.TenantID != ds.TenantID ||
		receipt.AppConfigVersion != ref.IdentityVersion {
		return nil, legacyFence("personal_handoff_receipt_changed")
	}
	expectedConnection := types.FeishuConnectionReferenceID(ref.Kind, ref.AppID, ref.IdentityVersion)
	if ref.Kind == types.FeishuUserOAuth {
		expectedConnection = types.FeishuConnectionReferenceID(ref.Kind, imported.UserID, imported.AppID,
			imported.OpenID, receipt.GrantEpoch)
	}
	if expectedConnection != ref.ConnectionID {
		return nil, legacyFence("personal_handoff_account_changed")
	}
	p.imported, p.receipt = imported, receipts[0]
	if p.resource.LogicalKnowledgeID != imported.KnowledgeID || p.attempt.ImportID != imported.ID ||
		p.attempt.Status != "indexed" || p.attempt.OwnerNonce != "" || p.attempt.LeaseUntil != nil {
		return nil, legacyFence("personal_handoff_owner_changed")
	}
	valid, err := VerifyPersonalExactPublishedDatabase(db.Statement.Context, db, receipt)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, legacyFence("personal_handoff_publication_changed")
	}
	return p, nil
}

// Called only after the ordinary source publisher verifies its new public rows,
// in that same transaction. Every earlier failure retains the old serving proof.
func completeExactSourceHandoff(tx *gorm.DB, p *ExactSourceHandoff) error {
	if p == nil {
		return nil
	}
	change := tx.Model(&types.TaskPendingOp{}).
		Where("id=? AND scope=? AND op=?", p.receipt.ID, types.PersonalExactReceiptScope, "published").
		Update("scope", types.PersonalExactSourceHistoryScope)
	if change.Error != nil {
		return change.Error
	}
	if change.RowsAffected != 1 {
		return legacyFence("personal_handoff_receipt_changed")
	}
	change = tx.Model(&types.FeishuPersonalResource{}).
		Where("import_id=? AND generation=? AND current_attempt_id=?", p.imported.ID,
			p.resource.Generation, p.attempt.ID).UpdateColumn("generation", p.resource.Generation+1)
	if change.Error != nil || change.RowsAffected != 1 {
		return errors.Join(change.Error, legacyFence("personal_handoff_generation_changed"))
	}
	change = tx.Model(&types.FeishuPersonalAttempt{}).
		Where("id=? AND status=? AND owner_nonce='' AND lease_until IS NULL", p.attempt.ID, "indexed").
		Update("status", "obsolete")
	if change.Error != nil || change.RowsAffected != 1 {
		return errors.Join(change.Error, legacyFence("personal_handoff_owner_changed"))
	}
	change = tx.Model(&types.FeishuPersonalImport{}).
		Where("id=? AND knowledge_id=? AND status=?", p.imported.ID, p.imported.KnowledgeID, "indexed").
		Updates(map[string]any{"status": "source_managed", "updated_at": time.Now()})
	if change.Error != nil || change.RowsAffected != 1 {
		return errors.Join(change.Error, legacyFence("personal_handoff_import_changed"))
	}
	return nil
}
