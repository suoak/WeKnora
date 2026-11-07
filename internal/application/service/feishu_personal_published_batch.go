package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// Split identities before selecting any body. Personal publications must not
// enter the old generic batch repository path; ordinary/shared reads retain it.
func (s *knowledgeService) exactPublishedSharedBatch(ctx context.Context, tenant uint64, ids []string) ([]*types.Knowledge, bool, error) {
	p, ok := s.personalRecovery.(*PersonalRecovery)
	if !ok || len(ids) == 0 {
		return nil, false, nil
	}
	if len(ids) > 4096 {
		return nil, true, errPersonalExactDocument
	}
	var personal []string
	if err := p.service.db.WithContext(ctx).Table("knowledges k").Select("k.id").Where("k.id IN ? AND (k.channel='feishu_personal' OR EXISTS (SELECT 1 FROM task_pending_ops o WHERE o.scope=? AND o.scope_id=k.id))", ids, types.PersonalExactReceiptScope).Scan(&personal).Error; err != nil {
		return nil, true, err
	}
	if len(personal) == 0 {
		return nil, false, nil
	}
	set := make(map[string]bool)
	rows := make(map[string]*types.Knowledge)
	for _, id := range personal {
		set[id] = true
		row, handled, err := s.readExactPublicKnowledge(ctx, id)
		if err != nil {
			return nil, true, err
		}
		if !handled || row == nil {
			return nil, true, errPersonalExactDocument
		}
		rows[id] = row
	}
	var ordinary []string
	for _, id := range ids {
		if !set[id] {
			ordinary = append(ordinary, id)
		}
	}
	if len(ordinary) > 0 {
		found, err := s.GetKnowledgeBatchWithSharedAccess(ctx, tenant, ordinary)
		if err != nil {
			return nil, true, err
		}
		for _, row := range found {
			if row != nil {
				rows[row.ID] = row
			}
		}
	}
	out := make([]*types.Knowledge, 0, len(rows))
	for _, id := range ids {
		if row := rows[id]; row != nil {
			out = append(out, row)
			delete(rows, id)
		}
	}
	return out, true, nil
}
