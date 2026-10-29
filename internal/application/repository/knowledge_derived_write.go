package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlog "gorm.io/gorm/logger"
)

// ErrDerivedWriteRejected is terminal for this task/version. Retrying the same
// payload must never acquire a newer publication's authority.
var ErrDerivedWriteRejected = errors.New("feishu_derived_write_rejected")

type derivedWriteKey struct{}
type derivedWriteSnapshot struct {
	mu                                                  sync.Mutex
	kid, resource, source, attempt, incarnation, digest string
	tenant                                              uint64
	generation, epoch                                   int64
	guard                                               types.FeishuSourceCommitGuard
	bound                                               context.Context
}

// KnowledgeDerivedVersion is an identity, not a grant. Queued work binds to it
// before model work, and the final write independently rechecks live authority.
func (r *knowledgeRepository) KnowledgeDerivedVersion(ctx context.Context, id string) (string, bool, error) {
	if !types.LegacyReadGuardsEnabled(r.db) {
		return "", false, nil
	}
	var row struct {
		ID, Attempt, Incarnation string
		Generation, Epoch        int64
	}
	err := r.db.WithContext(ctx).Raw(`SELECT r.id,r.current_attempt AS attempt,r.generation,
e.incarnation,e.generation AS epoch FROM feishu_legacy_resources r
JOIN document_access_epochs e ON e.knowledge_id=r.knowledge_id WHERE r.knowledge_id=?`, id).Scan(&row).Error
	if err != nil || row.ID == "" {
		return "", false, err
	}
	return fmt.Sprintf("%s/%d/%s/%s/%d", row.ID, row.Generation, row.Attempt, row.Incarnation, row.Epoch), true, nil
}

// BeginKnowledgeDerivedWrite captures an opaque optimistic snapshot. The
// binder is the existing Feishu source authorization path, not a caller claim.
// Network checks happen outside the transaction; its commit guard runs again
// under source/resource/document locks for every write.
func (r *knowledgeRepository) BeginKnowledgeDerivedWrite(ctx context.Context, id string, tenant uint64, expectedVersion string,
	binder func(context.Context, *types.DataSource) (context.Context, error),
) (context.Context, bool, error) {
	if !types.LegacyReadGuardsEnabled(r.db) {
		return ctx, false, nil
	}
	version, managed, err := r.KnowledgeDerivedVersion(ctx, id)
	if err != nil {
		return ctx, managed, err
	}
	if !managed {
		return ctx, false, nil
	}
	if r.db.Dialector.Name() != "postgres" {
		return ctx, true, ErrDerivedWriteRejected
	}
	if expectedVersion == "" || expectedVersion != version {
		return ctx, true, ErrDerivedWriteRejected
	}
	var resource types.FeishuLegacyResource
	err = r.db.WithContext(ctx).Where("knowledge_id=?", id).Take(&resource).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ctx, false, nil
	}
	if err != nil {
		return ctx, true, err
	}
	if resource.TenantID != tenant || binder == nil {
		return ctx, true, ErrDerivedWriteRejected
	}
	var source types.DataSource
	if err = r.db.WithContext(ctx).Where("id=? AND tenant_id=? AND legacy_access_state='active'", resource.DataSourceID, tenant).Take(&source).Error; err != nil {
		return ctx, true, ErrDerivedWriteRejected
	}
	bound, err := binder(ctx, &source)
	if err != nil {
		return ctx, true, fmt.Errorf("%w: source_authority_unavailable", ErrDerivedWriteRejected)
	}
	if err := types.CheckFeishuSourceRead(bound); err != nil {
		return ctx, true, fmt.Errorf("%w: remote_source_unavailable", ErrDerivedWriteRejected)
	}
	guard, ok := types.FeishuSourceGuardFromContext(bound)
	if !ok {
		return ctx, true, ErrDerivedWriteRejected
	}
	snapshot := &derivedWriteSnapshot{kid: id, tenant: tenant, resource: resource.ID, source: source.ID, guard: guard, bound: bound}
	err = r.db.WithContext(ctx).Session(&gorm.Session{Logger: gormlog.Discard}).Transaction(func(tx *gorm.DB) error {
		if err := snapshot.lock(tx); err != nil {
			return err
		}
		var live types.FeishuLegacyResource
		if err := tx.Where("id=?", snapshot.resource).Take(&live).Error; err != nil {
			return err
		}
		snapshot.generation, snapshot.attempt = live.Generation, live.CurrentAttempt
		if err := snapshot.readVersion(tx); err != nil {
			return err
		}
		captured := fmt.Sprintf("%s/%d/%s/%s/%d", snapshot.resource, snapshot.generation, snapshot.attempt, snapshot.incarnation, snapshot.epoch)
		if captured != expectedVersion {
			return ErrDerivedWriteRejected
		}
		return nil
	})
	if err != nil {
		return ctx, true, err
	}
	return context.WithValue(ctx, derivedWriteKey{}, snapshot), true, nil
}

func (s *derivedWriteSnapshot) lock(tx *gorm.DB) error {
	var source types.DataSource
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=? AND legacy_access_state='active' AND status IN ('active','error')", s.source, s.tenant).Take(&source).Error; err != nil {
		return ErrDerivedWriteRejected
	}
	var identity types.FeishuLegacyResource
	if err := tx.Where("id=? AND knowledge_id=? AND data_source_id=? AND tenant_id=?", s.resource, s.kid, s.source, s.tenant).Take(&identity).Error; err != nil {
		return ErrDerivedWriteRejected
	}
	key := fmt.Sprintf("%d:%s:%s:%s", identity.TenantID, identity.KnowledgeBaseID, identity.DataSourceID, identity.ExternalID)
	for _, key := range []string{key, "feishu-kid:" + s.kid} {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", key).Error; err != nil {
			return err
		}
	}
	var live types.FeishuLegacyResource
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", s.resource).Take(&live).Error; err != nil {
		return err
	}
	var count int64
	if err := tx.Table("feishu_legacy_attempts").Where("id=? AND resource_id=? AND generation=? AND status='published'", live.CurrentAttempt, live.ID, live.Generation).Count(&count).Error; err != nil {
		return err
	}
	if live.State != "active" || live.ActiveAttempt != live.CurrentAttempt || count != 1 ||
		(s.attempt != "" && (s.attempt != live.CurrentAttempt || s.generation != live.Generation)) {
		return ErrDerivedWriteRejected
	}
	// Lock the row before reading its epoch; both publication and derived
	// writes hold the same source/resource locks before reaching this point.
	var row struct{ ID string }
	if err := tx.Table("knowledges").Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id=? AND tenant_id=? AND deleted_at IS NULL AND parse_status='completed'", s.kid, s.tenant).Take(&row).Error; err != nil {
		return ErrDerivedWriteRejected
	}
	if s.guard == nil {
		return ErrDerivedWriteRejected
	}
	if err := s.guard(tx); err != nil {
		return fmt.Errorf("%w: source_authority_changed", ErrDerivedWriteRejected)
	}
	return nil
}

func (s *derivedWriteSnapshot) readVersion(tx *gorm.DB) error {
	var row struct {
		Digest, Incarnation string
		Epoch               int64
	}
	if err := tx.Raw(`SELECT md5(to_jsonb(k)::text) AS digest,e.incarnation,e.generation AS epoch
FROM knowledges k JOIN document_access_epochs e ON e.knowledge_id=k.id WHERE k.id=?`, s.kid).Scan(&row).Error; err != nil {
		return err
	}
	if row.Digest == "" || row.Incarnation == "" || row.Epoch < 1 {
		return ErrDerivedWriteRejected
	}
	s.digest, s.incarnation, s.epoch = row.Digest, row.Incarnation, row.Epoch
	return nil
}

func (r *knowledgeRepository) writeKnowledgeDerived(ctx context.Context, id string, values map[string]interface{}) (bool, error) {
	s, ok := ctx.Value(derivedWriteKey{}).(*derivedWriteSnapshot)
	if !ok {
		return false, nil
	}
	if s.kid != id {
		return true, ErrDerivedWriteRejected
	}
	for column := range values {
		switch column {
		case "description", "profile", "summary_status", "metadata", "updated_at":
		default:
			return true, ErrDerivedWriteRejected
		}
	}
	if err := types.CheckFeishuSourceRead(s.bound); err != nil {
		return true, fmt.Errorf("%w: remote_source_changed", ErrDerivedWriteRejected)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	patch, err := json.Marshal(values)
	if err != nil {
		return true, err
	}
	var next derivedWriteSnapshot
	err = r.db.WithContext(ctx).Session(&gorm.Session{Logger: gormlog.Discard}).Transaction(func(tx *gorm.DB) error {
		if err := s.lock(tx); err != nil {
			return err
		}
		next.kid = s.kid
		if err := next.readVersion(tx); err != nil {
			return err
		}
		if next.digest != s.digest || next.epoch != s.epoch || next.incarnation != s.incarnation {
			return ErrDerivedWriteRejected
		}
		// A one-use, transaction-bound database grant. The trigger independently
		// verifies source/publication state, OLD version and exact field changes.
		if err := tx.Exec(`INSERT INTO feishu_derived_write_grants
(transaction_id,knowledge_id,resource_id,generation,attempt_id,epoch,incarnation,row_digest,patch)
VALUES(txid_current(),?,?,?,?,?,?,?,?::jsonb)`, id, s.resource, s.generation, s.attempt, s.epoch, s.incarnation, s.digest, string(patch)).Error; err != nil {
			return err
		}
		result := tx.Model(&types.Knowledge{}).Where("id=? AND tenant_id=?", id, s.tenant).Updates(values)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDerivedWriteRejected
		}
		return next.readVersion(tx)
	})
	if err == nil {
		s.digest, s.epoch, s.incarnation = next.digest, next.epoch, next.incarnation
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) && state.SQLState() == "P0001" && strings.Contains(err.Error(), "Feishu derived write rejected") {
		err = fmt.Errorf("%w: database_version_fence", ErrDerivedWriteRejected)
	}
	return true, err
}
