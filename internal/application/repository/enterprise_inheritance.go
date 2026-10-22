package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrdinarySourceVerifier is a trusted ingestion-side classifier, never a
// browser form or free-form source metadata. External/unknown/sensitive sources
// must deny. A current version and an auditable rule reference are mandatory.
type OrdinarySourceVerifier interface {
	VerifyNewOrdinary(context.Context, *types.Knowledge, string) (string, error)
}

// InheritNewOrdinary reuses the existing KB policy's explicit delegation and
// the document epoch. Only ONE new resource since the approved KB membership
// snapshot is accepted. Existing denies/reviews cannot be overwritten. All
// writes and the KB revision rebase are atomic with audit; no fake human review.
// The normal upload/sync paths remain HOLD until their source verifier is bound.
func (r *EnterpriseAccessRepository) InheritNewOrdinary(ctx context.Context, doc string, classifier OrdinarySourceVerifier) error {
	if classifier == nil || doc == "" {
		return errors.New("trusted ordinary source classification unavailable: HOLD")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var d types.Knowledge
		if err := tx.Where("id=?", doc).Take(&d).Error; err != nil {
			return err
		}
		if d.Type != "file" && d.Type != "manual" || d.Channel != "web" && d.Channel != "api" || d.ParseStatus != "completed" || d.EnableStatus != "enabled" {
			return errors.New("source not inheritable")
		}
		var kb types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=? AND is_temporary=false", d.KnowledgeBaseID, d.TenantID).Take(&kb).Error; err != nil {
			return err
		}
		if kb.Type != "document" {
			return errors.New("not an ordinary document KB")
		}
		var published int64
		if err := tx.Table("tenant_portal_configs pc").Joins("JOIN tenants t ON t.id=pc.tenant_id AND t.status='active' AND t.deleted_at IS NULL").Where("pc.tenant_id=? AND pc.status='published'", kb.TenantID).Count(&published).Error; err != nil {
			return err
		}
		if published != 1 {
			return errors.New("Portal not published")
		}
		p, err := loadEnterprisePolicy(tx, types.EnterprisePolicyTarget{Kind: "kb", ID: kb.ID}, true)
		if err != nil {
			return err
		}
		approved := func(p types.EnterprisePolicy) bool {
			return p.State == "approved" && p.Policy == "inherit" && p.ContentClass == "ordinary" && p.SourceACLState == "local" && p.ProposedBy != "" && p.ReviewedBy != nil && *p.ReviewedBy != "" && *p.ReviewedBy != p.ProposedBy && p.ReviewedAt != nil && p.Revision > 0
		}
		if !approved(p) || !p.InheritNewOrdinary || !d.CreatedAt.After(*p.ReviewedAt) {
			return errors.New("KB inheritance not approved")
		}
		space, err := loadEnterprisePolicy(tx, types.EnterprisePolicyTarget{Kind: "space", ID: strconv.FormatUint(kb.TenantID, 10)}, true)
		if err != nil {
			return err
		}
		spaceVersion, _, err := resourceVersion(tx, types.EnterprisePolicyTarget{Kind: "space", ID: strconv.FormatUint(kb.TenantID, 10)})
		if err != nil {
			return err
		}
		if !approved(space) || space.ResourceVersion != spaceVersion {
			return errors.New("space approval stale")
		}
		// Recreate the exact old KB hash with the one new document excluded.
		var children []map[string]interface{}
		if err := tx.Table("knowledges").Select("id,tenant_id").Where("knowledge_base_id=? AND id<>? AND deleted_at IS NULL", kb.ID, d.ID).Order("id").Find(&children).Error; err != nil {
			return err
		}
		row := struct {
			TenantID  uint64
			KBID      string
			UpdatedAt time.Time
		}{kb.TenantID, kb.ID, kb.UpdatedAt}
		epoch := struct {
			Generation  int64
			Incarnation string
		}{}
		encoded, err := json.Marshal(struct {
			Target   types.EnterprisePolicyTarget
			Row      interface{}
			Children interface{}
			Epoch    interface{}
		}{types.EnterprisePolicyTarget{Kind: "kb", ID: kb.ID}, row, children, epoch})
		if err != nil {
			return err
		}
		hash := sha256.Sum256(encoded)
		if p.ResourceVersion != hex.EncodeToString(hash[:]) {
			return types.ErrEnterprisePolicyConflict
		}
		old, err := loadEnterprisePolicy(tx, types.EnterprisePolicyTarget{Kind: "knowledge", ID: doc}, true)
		if err != nil {
			return err
		}
		if old.Revision != 0 {
			return errors.New("existing resource policy cannot inherit")
		}
		version, _, err := resourceVersion(tx, types.EnterprisePolicyTarget{Kind: "knowledge", ID: doc})
		if err != nil {
			return err
		}
		reference, err := classifier.VerifyNewOrdinary(ctx, &d, version)
		if err != nil || reference == "" || len(reference) > 256 {
			return errors.New("source classification denied")
		}
		// Recheck after classifier I/O; its proof is bound to the actual epoch.
		current, _, err := resourceVersion(tx, types.EnterprisePolicyTarget{Kind: "knowledge", ID: doc})
		if err != nil || current != version {
			return types.ErrEnterprisePolicyConflict
		}
		now := time.Now().UTC()
		next := p
		next.Revision = 1
		next.InheritNewOrdinary = false
		next.ResourceVersion = version
		next.UpdatedAt = now
		next.Reason = "inherited KB rule: " + reference
		values := enterprisePolicyValues(next)
		values["knowledge_id"] = doc
		insert := tx.Table("knowledge_access_policies").Clauses(clause.OnConflict{DoNothing: true}).Create(values)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected != 1 {
			return types.ErrEnterprisePolicyConflict
		}
		kbVersion, _, err := resourceVersion(tx, types.EnterprisePolicyTarget{Kind: "kb", ID: kb.ID})
		if err != nil {
			return err
		}
		write := tx.Table("kb_publication_policies").Where("knowledge_base_id=? AND revision=? AND state='approved'", kb.ID, p.Revision).Updates(map[string]any{"resource_version": kbVersion, "revision": p.Revision + 1, "updated_at": now})
		if write.Error != nil {
			return write.Error
		}
		if write.RowsAffected != 1 {
			return types.ErrEnterprisePolicyConflict
		}
		details, _ := json.Marshal(map[string]any{"rule_reference": reference, "kb_revision": p.Revision + 1, "resource_version": version, "basis": "previously approved inheritance; no new human review"})
		return NewAuditLogRepository(tx).Create(ctx, &types.AuditLog{TenantID: kb.TenantID, ActorUserID: "", Action: "enterprise.policy_inherit", ScopeType: "tenant", ScopeID: strconv.FormatUint(kb.TenantID, 10), TargetType: "enterprise_knowledge", TargetID: doc, Outcome: types.AuditOutcomeSuccess, Details: types.JSON(details), CreatedAt: now})
	})
}
