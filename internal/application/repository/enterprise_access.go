package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EnterpriseAccessRepository struct {
	db       *gorm.DB
	versions *documentVersionCache
}

func NewEnterpriseAccessRepository(db *gorm.DB) *EnterpriseAccessRepository {
	return &EnterpriseAccessRepository{db: db, versions: newDocumentVersionCache(65536)}
}

// resourceVersion binds review to persisted resource/chunk revision metadata.
// No document content, path, key, or source URL is fetched for authorization.
func resourceVersion(tx *gorm.DB, target types.EnterprisePolicyTarget) (string, uint64, error) {
	var row struct {
		TenantID  uint64
		KBID      string
		UpdatedAt time.Time
	}
	var query *gorm.DB
	switch target.Kind {
	case "space":
		query = tx.Table("tenants").Select("id AS tenant_id, updated_at").Where("id = ? AND deleted_at IS NULL AND status = ?", target.ID, "active")
	case "kb":
		query = tx.Table("knowledge_bases").Select("tenant_id, id AS kb_id, updated_at").Where("id = ? AND deleted_at IS NULL AND is_temporary = ?", target.ID, false)
	case "knowledge":
		query = tx.Table("knowledges").Select("tenant_id, knowledge_base_id AS kb_id, updated_at").Where("id = ? AND deleted_at IS NULL", target.ID)
	default:
		return "", 0, errors.New("invalid enterprise resource kind")
	}
	if err := query.Take(&row).Error; err != nil {
		return "", 0, err
	}
	if row.TenantID == 0 || row.UpdatedAt.IsZero() {
		return "", 0, errors.New("resource version unavailable")
	}
	var children []map[string]interface{}
	var epoch struct {
		Generation  int64
		Incarnation string
	}
	if target.Kind == "knowledge" {
		// Database triggers cover old workers, bulk/raw SQL and writes which
		// preserve timestamps. Missing migration/trigger coverage is HOLD.
		if err := checkDocumentEpochCoverage(tx); err != nil {
			return "", 0, err
		}
		if err := tx.Table("document_access_epochs").Where("knowledge_id=?", target.ID).Take(&epoch).Error; err != nil {
			return "", 0, err
		}
		if epoch.Generation <= 0 || len(epoch.Incarnation) != 32 {
			return "", 0, errors.New("document epoch unavailable")
		}
		if err := tx.Table("chunks").Select("id, content_revision, updated_at, is_enabled, deleted_at").Where("knowledge_id = ?", target.ID).Order("id").Find(&children).Error; err != nil {
			return "", 0, err
		}
	} else if target.Kind == "kb" {
		if err := tx.Table("knowledges").Select("id, tenant_id").Where("knowledge_base_id = ? AND deleted_at IS NULL", target.ID).Order("id").Find(&children).Error; err != nil {
			return "", 0, err
		}
	}
	encoded, err := json.Marshal(struct {
		Target   types.EnterprisePolicyTarget
		Row      interface{}
		Children interface{}
		Epoch    interface{}
	}{target, row, children, epoch})
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), row.TenantID, nil
}

func loadEnterprisePolicy(tx *gorm.DB, target types.EnterprisePolicyTarget, lock bool) (types.EnterprisePolicy, error) {
	table, column, ok := target.Storage()
	if !ok {
		return types.EnterprisePolicy{}, errors.New("invalid enterprise policy target")
	}
	var p types.EnterprisePolicy
	q := tx.Table(table).Where(column+" = ?", target.ID)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return types.EnterprisePolicy{}, nil
	}
	return p, err
}

// ChangeEnterprisePolicy is a repository primitive. Admission must use
// access.ChangeEnterprisePolicy and its live, explicit business-role authority.
func (r *EnterpriseAccessRepository) ChangeEnterprisePolicy(ctx context.Context, c types.EnterprisePolicyChange) (*types.EnterprisePolicy, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("enterprise policy store unavailable")
	}
	table, column, ok := c.Target.Storage()
	if !ok || strings.TrimSpace(c.ActorID) == "" || strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 512 {
		return nil, errors.New("invalid enterprise policy change")
	}
	var result types.EnterprisePolicy
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var actor struct{ IsActive bool }
		if err := tx.Table("users").Select("is_active").Where("id = ? AND deleted_at IS NULL", c.ActorID).Take(&actor).Error; err != nil {
			return err
		}
		if !actor.IsActive {
			return errors.New("inactive enterprise policy actor")
		}
		version, tenantID, err := resourceVersion(tx, c.Target)
		if err != nil {
			return err
		}
		old, err := loadEnterprisePolicy(tx, c.Target, true)
		if err != nil {
			return err
		}
		if c.Action != "revoke" && old.Revision != c.ExpectedRevision {
			return types.ErrEnterprisePolicyConflict
		}
		next := old
		now := time.Now().UTC()
		switch c.Action {
		case "propose":
			if c.Policy != types.EnterprisePolicyInherit && c.Policy != types.EnterprisePolicyRestricted && c.Policy != types.EnterprisePolicyMCPBlocked && c.Policy != types.EnterprisePolicyDeny && c.Policy != types.EnterprisePolicyPending {
				return errors.New("invalid publication policy")
			}
			if c.ContentClass != "ordinary" && c.ContentClass != "mixed" && c.ContentClass != "unknown" {
				return errors.New("invalid content class")
			}
			if c.SourceACLState != "local" && c.SourceACLState != "verified" && c.SourceACLState != "unknown" && c.SourceACLState != "revoked" {
				return errors.New("invalid source ACL state")
			}
			next.State, next.Policy, next.ContentClass, next.SourceACLState = types.EnterprisePolicyPending, c.Policy, c.ContentClass, c.SourceACLState
			next.ProposedBy, next.ReviewedBy, next.ReviewedAt = c.ActorID, nil, nil
			next.InheritNewOrdinary = c.InheritNewOrdinary
			next.ResourceVersion = version
		case "approve":
			if old.State != types.EnterprisePolicyPending || old.ProposedBy == "" || old.ProposedBy == c.ActorID || old.ResourceVersion != version {
				return types.ErrEnterprisePolicyConflict
			}
			next.State, next.ReviewedBy, next.ReviewedAt = types.EnterprisePolicyApproved, &c.ActorID, &now
		case "revoke":
			// Deliberately acts on the current locked revision, not an old
			// client revision. Stale approvals can never restore this row.
			next.State = types.EnterprisePolicyRevoked
			if old.Revision == 0 {
				next.Policy, next.ContentClass, next.SourceACLState = types.EnterprisePolicyPending, "unknown", "unknown"
			}
		default:
			return errors.New("invalid enterprise policy action")
		}
		next.Revision, next.UpdatedBy, next.UpdatedAt, next.Reason = old.Revision+1, c.ActorID, now, c.Reason
		if old.Revision == 0 {
			values := enterprisePolicyValues(next)
			values[column] = c.Target.ID
			insert := tx.Table(table).Clauses(clause.OnConflict{DoNothing: true}).Create(values)
			if insert.Error != nil {
				return insert.Error
			}
			if insert.RowsAffected != 1 {
				return types.ErrEnterprisePolicyConflict
			}
		} else {
			update := tx.Table(table).Where(column+" = ? AND revision = ?", c.Target.ID, old.Revision).Updates(enterprisePolicyValues(next))
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return types.ErrEnterprisePolicyConflict
			}
		}
		details, err := json.Marshal(map[string]interface{}{"resource_kind": c.Target.Kind, "resource_id": c.Target.ID, "old_state": old.State, "new_state": next.State, "policy": next.Policy, "old_revision": old.Revision, "revision": next.Revision, "resource_version": next.ResourceVersion, "reason": c.Reason})
		if err != nil {
			return err
		}
		if err := NewAuditLogRepository(tx).Create(ctx, &types.AuditLog{TenantID: tenantID, ActorUserID: c.ActorID, Action: types.AuditAction("enterprise.policy_" + c.Action), ScopeType: "tenant", ScopeID: strconv.FormatUint(tenantID, 10), TargetType: "enterprise_" + c.Target.Kind, TargetID: c.Target.ID, Outcome: types.AuditOutcomeSuccess, Details: types.JSON(details), CreatedAt: now}); err != nil {
			return err
		}
		result = next
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func enterprisePolicyValues(p types.EnterprisePolicy) map[string]interface{} {
	var proposer interface{}
	if p.ProposedBy != "" {
		proposer = p.ProposedBy
	}
	return map[string]interface{}{"state": p.State, "policy": p.Policy, "content_class": p.ContentClass, "source_acl_state": p.SourceACLState, "inherit_new_ordinary": p.InheritNewOrdinary, "revision": p.Revision, "proposed_by": proposer, "reviewed_by": p.ReviewedBy, "reviewed_at": p.ReviewedAt, "updated_by": p.UpdatedBy, "updated_at": p.UpdatedAt, "reason": p.Reason, "resource_version": p.ResourceVersion}
}

func (r *EnterpriseAccessRepository) LoadEnterpriseReadSnapshot(ctx context.Context, userID string, keyID uint64, kbID string) (*types.EnterpriseReadSnapshot, error) {
	return r.loadReadSnapshot(ctx, userID, keyID, kbID, true)
}

// LoadDocumentReadSnapshot shares the policy facts with public reads but leaves
// individual document decisions to the ACL evaluator. No public grant is made.
func (r *EnterpriseAccessRepository) LoadDocumentReadSnapshot(ctx context.Context, userID string, keyID uint64, kbID string) (*types.EnterpriseReadSnapshot, error) {
	return r.loadReadSnapshot(ctx, userID, keyID, kbID, false)
}

func (r *EnterpriseAccessRepository) loadReadSnapshot(ctx context.Context, userID string, keyID uint64, kbID string, publicOnly bool) (*types.EnterpriseReadSnapshot, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("enterprise policy store unavailable")
	}
	s := &types.EnterpriseReadSnapshot{UserID: userID, KBID: kbID}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user struct{ IsActive bool }
		if err := tx.Table("users").Select("is_active").Where("id = ? AND deleted_at IS NULL", userID).Take(&user).Error; err != nil {
			return err
		}
		s.UserActive = user.IsActive
		var credential struct {
			types.EnterpriseCredential
			CapabilitiesJSON string `gorm:"column:capabilities"`
		}
		if keyID != 0 {
			if err := tx.Table("tenant_api_keys").Select("id, owner_user_id, scope_type, capabilities, revoked_at, expires_at, full_access").Where("id = ?", keyID).Take(&credential).Error; err != nil {
				return err
			}
			// SQLite TEXT and PostgreSQL JSONB must both be parsed strictly. The
			// legacy StringArray scanner ignores non-byte driver values.
			s.Credential = credential.EnterpriseCredential
			if err := json.Unmarshal([]byte(credential.CapabilitiesJSON), &s.Credential.Capabilities); err != nil {
				return err
			}
		} else if publicOnly {
			return errors.New("public key required")
		}
		var anchor struct {
			TenantID    uint64
			Type        string
			IsTemporary bool
			Status      string
		}
		if err := tx.Table("knowledge_bases kb").Select("kb.tenant_id, kb.type, kb.is_temporary, pc.status").Joins("JOIN tenants t ON t.id=kb.tenant_id AND t.deleted_at IS NULL AND t.status='active'").Joins("LEFT JOIN tenant_portal_configs pc ON pc.tenant_id=t.id").Where("kb.id=? AND kb.deleted_at IS NULL", kbID).Take(&anchor).Error; err != nil {
			return err
		}
		s.TenantID, s.KBType, s.KBTemporary, s.TenantActive, s.PortalPublished = anchor.TenantID, anchor.Type, anchor.IsTemporary, true, anchor.Status == "published"
		var err error
		s.Space, err = loadEnterprisePolicy(tx, types.EnterprisePolicyTarget{Kind: "space", ID: strconv.FormatUint(s.TenantID, 10)}, false)
		if err != nil {
			return err
		}
		s.KB, err = loadEnterprisePolicy(tx, types.EnterprisePolicyTarget{Kind: "kb", ID: kbID}, false)
		if err != nil {
			return err
		}
		s.PortalPublicBlocked = types.ExplicitPortalPublicBlock(s.KB) || types.ExplicitPortalPublicBlock(s.Space)
		spaceVersion, _, err := resourceVersion(tx, types.EnterprisePolicyTarget{Kind: "space", ID: strconv.FormatUint(s.TenantID, 10)})
		if err != nil {
			return err
		}
		kbVersion, _, err := resourceVersion(tx, types.EnterprisePolicyTarget{Kind: "kb", ID: kbID})
		if err != nil {
			return err
		}
		if s.Space.ResourceVersion != spaceVersion || s.KB.ResourceVersion != kbVersion {
			if publicOnly {
				return fmt.Errorf("%w: resource changed since review", types.ErrEnterprisePolicyConflict)
			}
			if s.Space.ResourceVersion != spaceVersion {
				s.Space.State = types.EnterprisePolicyPending
			}
			if s.KB.ResourceVersion != kbVersion {
				s.KB.State = types.EnterprisePolicyPending
			}
		}
		if err := tx.Table("knowledges").Select("id,tenant_id,knowledge_base_id,type,parse_status,enable_status,channel").Where("knowledge_base_id=? AND deleted_at IS NULL", kbID).Order("id").Find(&s.Documents).Error; err != nil {
			return err
		}
		if err := populateDocumentVersionsCached(tx, s.Documents, r.versions); err != nil {
			return err
		}

		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return s, nil
}
