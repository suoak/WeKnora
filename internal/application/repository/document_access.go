package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func checkDocumentEpochCoverage(tx *gorm.DB) error {
	var count int64
	var err error
	switch tx.Dialector.Name() {
	case "sqlite":
		err = tx.Raw(`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name IN ('document_epoch_knowledges_insert','document_epoch_knowledges_update','document_epoch_knowledges_delete','document_epoch_chunks_insert','document_epoch_chunks_update','document_epoch_chunks_delete')`).Scan(&count).Error
		if err == nil && count == 6 {
			return nil
		}
	case "postgres":
		err = tx.Raw(`SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgenabled='O' AND tgrelid IN ('knowledges'::regclass,'chunks'::regclass) AND tgname IN ('document_epoch_knowledges','document_epoch_chunks','document_epoch_knowledges_truncate','document_epoch_chunks_truncate')`).Scan(&count).Error
		if err == nil && count == 4 {
			return nil
		}
	}
	return errors.New("document epoch trigger coverage unavailable: HOLD")
}

func (r *EnterpriseAccessRepository) LoadDocumentSubjectGrant(ctx context.Context, doc, user string, operation types.DocumentOperation) (*types.DocumentSubjectGrant, error) {
	var grant types.DocumentSubjectGrant
	err := r.db.WithContext(ctx).Table("document_subject_grants").Where("knowledge_id=? AND user_id=? AND operation=?", doc, user, operation).Take(&grant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &types.DocumentSubjectGrant{}, nil
	}
	return &grant, err
}

// ChangeDocumentSubjectGrant is a persistence primitive. Call through the
// access admission function; never expose this method directly to a handler.
// Revocations ignore stale client revisions; stale approvals cannot win CAS.
func (r *EnterpriseAccessRepository) ChangeDocumentSubjectGrant(ctx context.Context, c types.DocumentGrantChange) (*types.DocumentSubjectGrant, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("document grant store unavailable")
	}
	if !c.Operation.Valid() || c.KnowledgeID == "" || c.UserID == "" || c.ActorID == "" || c.ActorID == c.UserID || c.Reason == "" || len(c.Reason) > 512 || (c.State != "approved" && c.State != "revoked") {
		return nil, errors.New("invalid document grant")
	}
	var result types.DocumentSubjectGrant
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, id := range []string{c.ActorID, c.UserID} {
			var active bool
			if err := tx.Table("users").Select("is_active").Where("id=? AND deleted_at IS NULL", id).Take(&active).Error; err != nil {
				return err
			}
			if !active {
				return errors.New("inactive grant subject")
			}
		}
		var old types.DocumentSubjectGrant
		q := tx.Table("document_subject_grants").Where("knowledge_id=? AND user_id=? AND operation=?", c.KnowledgeID, c.UserID, c.Operation)
		err := q.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&old).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		target := types.EnterprisePolicyTarget{Kind: "knowledge", ID: c.KnowledgeID}
		version, tenant, err := resourceVersion(tx, target)
		if err != nil {
			return err
		}
		if c.State == "approved" {
			if c.Revision != old.Revision || c.ResourceVersion != version {
				return types.ErrEnterprisePolicyConflict
			}
			policy, err := loadEnterprisePolicy(tx, target, true)
			if err != nil {
				return err
			}
			if policy.State != "approved" || policy.Policy == "deny" || policy.Policy == "pending_review" || policy.ResourceVersion != version {
				return errors.New("document not approved")
			}
		}
		result = types.DocumentSubjectGrant{KnowledgeID: c.KnowledgeID, UserID: c.UserID, Operation: c.Operation, State: c.State, Revision: old.Revision + 1, ResourceVersion: version}
		values := map[string]interface{}{"knowledge_id": c.KnowledgeID, "user_id": c.UserID, "operation": c.Operation, "state": result.State, "revision": result.Revision, "resource_version": version, "updated_by": c.ActorID, "reason": c.Reason}
		var write *gorm.DB
		if c.State == "revoked" {
			// Also covers the missing-row race: a concurrent first approval
			// cannot make a revocation lose an INSERT ... DO NOTHING race.
			write = tx.Table("document_subject_grants").Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "knowledge_id"}, {Name: "user_id"}, {Name: "operation"}}, DoUpdates: clause.Assignments(map[string]interface{}{"state": "revoked", "revision": gorm.Expr("document_subject_grants.revision+1"), "resource_version": version, "updated_by": c.ActorID, "reason": c.Reason})}).Create(values)
		} else if old.Revision == 0 {
			write = tx.Table("document_subject_grants").Clauses(clause.OnConflict{DoNothing: true}).Create(values)
		} else {
			write = tx.Table("document_subject_grants").Where("knowledge_id=? AND user_id=? AND operation=? AND revision=?", c.KnowledgeID, c.UserID, c.Operation, old.Revision).Updates(values)
		}
		if write.Error != nil {
			return write.Error
		}
		if write.RowsAffected != 1 {
			return types.ErrEnterprisePolicyConflict
		}
		if c.State == "revoked" {
			if err := tx.Table("document_subject_grants").Where("knowledge_id=? AND user_id=? AND operation=?", c.KnowledgeID, c.UserID, c.Operation).Take(&result).Error; err != nil {
				return err
			}
		}
		details, err := json.Marshal(map[string]interface{}{"user_id": c.UserID, "operation": c.Operation, "state": result.State, "revision": result.Revision, "resource_version": version, "reason": c.Reason})
		if err != nil {
			return err
		}
		return NewAuditLogRepository(tx).Create(ctx, &types.AuditLog{TenantID: tenant, ActorUserID: c.ActorID, Action: "enterprise.document_grant", ScopeType: "tenant", ScopeID: strconv.FormatUint(tenant, 10), TargetType: "knowledge", TargetID: c.KnowledgeID, Outcome: types.AuditOutcomeSuccess, Details: types.JSON(details), CreatedAt: time.Now().UTC()})
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
