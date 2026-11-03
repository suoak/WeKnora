package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const wikiStateScope = "wiki_processing_state"

// WikiProcessingBlock is metadata only. Original work, payloads, claims and
// attempt identities remain in their original lanes, untouched.
type WikiProcessingBlock struct {
	Reason     string    `json:"reason"`
	CheckedAt  time.Time `json:"checked_at"`
	RetryAfter time.Time `json:"retry_after"`
}

func ReadWikiProcessingBlock(ctx context.Context, repo interfaces.WikiPageRepository, kb string) (*WikiProcessingBlock, error) {
	r, ok := repo.(*wikiPageRepository)
	if !ok {
		return nil, nil
	}
	var row types.TaskPendingOp
	err := r.db.WithContext(ctx).Where("scope=? AND scope_id=? AND task_type=?", wikiStateScope, kb, "wiki:state").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var block WikiProcessingBlock
	if err := json.Unmarshal(row.Payload, &block); err != nil {
		return nil, err
	}
	return &block, nil
}

// Serialize on KB metadata, not on protected source content. No source read
// authority is issued by this status update. Resolution is retained in the
// same record for audit; it never marks document work as complete.
func WriteWikiProcessingBlock(ctx context.Context, repo interfaces.WikiPageRepository, tenant uint64, kb, reason string) error {
	r, ok := repo.(*wikiPageRepository)
	if !ok {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var anchor struct{ ID string }
		query := tx.Table("knowledge_bases").Select("id").Where("id=? AND tenant_id=? AND deleted_at IS NULL", kb, tenant)
		if tx.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Take(&anchor).Error; err != nil {
			return err
		}
		block := WikiProcessingBlock{Reason: reason, CheckedAt: time.Now().UTC()}
		if reason != "" {
			block.RetryAfter = block.CheckedAt.Add(10 * time.Minute)
		}
		payload, err := json.Marshal(block)
		if err != nil {
			return err
		}
		var row types.TaskPendingOp
		err = tx.Where("scope=? AND scope_id=? AND task_type=?", wikiStateScope, kb, "wiki:state").Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if reason == "" {
				return nil
			}
			return tx.Create(&types.TaskPendingOp{TenantID: tenant, TaskType: "wiki:state", Scope: wikiStateScope, ScopeID: kb, Op: "blocked", DedupKey: kb, Payload: payload, EnqueuedAt: block.CheckedAt}).Error
		}
		if err != nil {
			return err
		}
		op := "blocked"
		if reason == "" {
			op = "resolved"
		}
		return tx.Model(&types.TaskPendingOp{}).Where("id=?", row.ID).Updates(map[string]any{"op": op, "payload": payload}).Error
	})
}
