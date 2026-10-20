package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"hash/fnv"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *knowledgeRepository) AcceptKnowledgeDispatch(ctx context.Context, k *types.Knowledge, op *types.TaskPendingOp) error {
	committing := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(k).Error; err != nil {
			return err
		}
		if err := tx.Create(op).Error; err != nil {
			return err
		}
		committing = true
		return nil
	})
	if err != nil {
		return &types.KnowledgeDispatchAdmissionError{Cause: err, CommitUnknown: committing}
	}
	return nil
}

func (r *knowledgeRepository) FindFeishuPersonalKnowledge(ctx context.Context, tenant uint64, kb, user, resource string) (*types.Knowledge, error) {
	var rows []*types.Knowledge
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id = ? AND channel = ? AND metadata->>'feishu_imported_by' = ? AND metadata->>'feishu_resource_id' = ?", tenant, kb, "feishu_personal", user, resource).Limit(2).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) > 1 {
		return nil, errors.New("ambiguous personal source identity requires attention")
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// A primary-key row lock serializes legacy intent reconstruction. No network
// operations occur in this transaction. Existing immutable intent always wins.
func (r *knowledgeRepository) EnsureKnowledgeDispatch(ctx context.Context, k *types.Knowledge, op *types.TaskPendingOp) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&types.Knowledge{}).Where("id = ? AND tenant_id = ? AND file_hash = ? AND file_path = ?", k.ID, k.TenantID, k.FileHash, k.FilePath)
		result := q.UpdateColumn("updated_at", time.Now())
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("dispatch source changed")
		}
		var existing types.TaskPendingOp
		err := tx.Where("scope = ? AND scope_id = ? AND tenant_id = ?", types.KnowledgeDispatchScope, k.ID, k.TenantID).First(&existing).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(op).Error
	})
}

func (r *knowledgeRepository) GetKnowledgeDispatch(ctx context.Context, tenant uint64, id string) (*types.TaskPendingOp, error) {
	var op types.TaskPendingOp
	err := r.db.WithContext(ctx).Where("scope = ? AND scope_id = ? AND tenant_id = ?", types.KnowledgeDispatchScope, id, tenant).First(&op).Error
	return &op, err
}

func (r *knowledgeRepository) ListKnowledgeDispatches(ctx context.Context, after int64, limit int) ([]*types.TaskPendingOp, error) {
	var ops []*types.TaskPendingOp
	err := r.db.WithContext(ctx).Where("scope = ? AND id > ? AND op IN ?", types.KnowledgeDispatchScope, after, []string{"pending", "dispatched", "processing", "recovery", "processed"}).Order("id").Limit(limit).Find(&ops).Error
	return ops, err
}

func (r *knowledgeRepository) SetKnowledgeDispatchState(ctx context.Context, id int64, state string) error {
	// Publication acknowledgements must never overwrite a worker's newer state.
	q := r.db.WithContext(ctx).Model(&types.TaskPendingOp{}).Where("id = ? AND scope = ?", id, types.KnowledgeDispatchScope)
	if state == "dispatched" {
		q = q.Where("op IN ?", []string{"pending", "recovery", "dispatched"})
	}
	return q.UpdateColumn("op", state).Error
}

type dispatchLiteLock struct {
	gate chan struct{}
	refs int
}

var dispatchLiteLocks = struct {
	sync.Mutex
	entries map[string]*dispatchLiteLock
}{entries: make(map[string]*dispatchLiteLock)}

func (r *knowledgeRepository) RecordKnowledgeDispatchFailure(ctx context.Context, id int64, category string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var op types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND scope = ?", id, types.KnowledgeDispatchScope).First(&op).Error; err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(op.Payload, &fields); err != nil {
			return err
		}
		fields["last_error_category"], _ = json.Marshal(category)
		payload, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		return tx.Model(&types.TaskPendingOp{}).Where("id = ?", id).Updates(map[string]any{"fail_count": gorm.Expr("fail_count + 1"), "payload": json.RawMessage(payload)}).Error
	})
}

func (r *knowledgeRepository) WithKnowledgeDispatchLock(ctx context.Context, id string, fn func(context.Context) error) error {
	if r.db.Dialector.Name() != "postgres" {
		// Lite is a single-process executor; unlike a timed lease, this mutex
		// cannot expire while the handler is still writing chunks.
		dispatchLiteLocks.Lock()
		entry := dispatchLiteLocks.entries[id]
		if entry == nil {
			entry = &dispatchLiteLock{gate: make(chan struct{}, 1)}
			dispatchLiteLocks.entries[id] = entry
		}
		entry.refs++
		dispatchLiteLocks.Unlock()
		defer func() {
			dispatchLiteLocks.Lock()
			defer dispatchLiteLocks.Unlock()
			entry.refs--
			if entry.refs == 0 {
				delete(dispatchLiteLocks.entries, id)
			}
		}()
		select {
		case entry.gate <- struct{}{}:
			defer func() { <-entry.gate }()
		case <-ctx.Done():
			return ctx.Err()
		}
		return fn(ctx)
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte("knowledge-dispatch:" + id))
	key := int64(h.Sum64())
	return r.db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
		var acquired bool
		if err := conn.Raw("SELECT pg_try_advisory_lock(?)", key).Scan(&acquired).Error; err != nil {
			return err
		}
		if !acquired {
			return errors.New("knowledge dispatch is already processing")
		}
		workerCtx, stopWorker := context.WithCancel(ctx)
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-workerCtx.Done():
					return
				case <-ticker.C:
					probe, cancel := context.WithTimeout(workerCtx, 2*time.Second)
					err := conn.WithContext(probe).Exec("SELECT 1").Error
					cancel()
					if err != nil {
						stopWorker()
						return
					}
				}
			}
		}()
		defer func() { stopWorker(); <-stopped }()
		defer func() {
			release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			// If unlock fails, discard the session rather than returning a locked
			// connection to the pool (database/sql closes ErrBadConn connections).
			if err := conn.WithContext(release).Exec("SELECT pg_advisory_unlock(?)", key).Error; err != nil {
				if sqlConn, ok := conn.Statement.ConnPool.(interface{ Raw(func(any) error) error }); ok {
					_ = sqlConn.Raw(func(any) error { return driver.ErrBadConn })
				}
			}
		}()
		return errors.Join(fn(workerCtx), workerCtx.Err())
	})
}
