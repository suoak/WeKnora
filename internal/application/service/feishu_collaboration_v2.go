package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

// Contact is an owner-entered label. It sends no message, grants no source
// permission, and never exposes the failed resource to another account.
type FeishuCollaboration struct {
	ID        string    `json:"id"`
	BatchID   string    `json:"batch_id"`
	ItemID    string    `json:"item_id"`
	ActorID   string    `json:"-"`
	Action    string    `json:"action"`
	Contact   string    `json:"contact"`
	Note      string    `json:"note"`
	Result    string    `json:"result"`
	CreatedAt time.Time `json:"created_at"`
}

func (FeishuCollaboration) TableName() string { return "feishu_permission_collaboration" }

func (s *FeishuPersonalService) Collaboration(ctx context.Context, batchID string) ([]FeishuCollaboration, error) {
	if _, err := s.Batch(ctx, batchID); err != nil {
		return nil, err
	}
	rows := []FeishuCollaboration{}
	err := s.db.WithContext(ctx).Where("batch_id = ?", batchID).Order("created_at DESC, id DESC").Limit(100).Find(&rows).Error
	return rows, err
}

func (s *FeishuPersonalService) Collaborate(ctx context.Context, batchID, itemID, action, contact, note string) (*FeishuCollaboration, error) {
	if action != "note" && action != "reported" && action != "recheck" || len(contact) > 128 || len(note) > 512 || action != "recheck" && strings.TrimSpace(note) == "" {
		return nil, errors.New("invalid_permission_collaboration")
	}
	view, err := s.Batch(ctx, batchID)
	if err != nil {
		return nil, err
	}
	var item *types.FeishuBatchItem
	for i := range view.Items {
		if view.Items[i].ID == itemID {
			item = &view.Items[i]
			break
		}
	}
	if item == nil {
		return nil, access.ErrNotFound
	}
	if item.Status != "blocked" && item.Status != "failed" {
		return nil, errors.New("permission_issue_required")
	}
	result := "awaiting_source_permission"
	if action == "reported" {
		result = "reported_unverified"
	}
	if action == "recheck" {
		rows, e := s.Preview(ctx, view.Batch.KBID, view.Batch.AuthMode, view.Batch.ConnectionID, []string{item.URL})
		if e != nil {
			return nil, e
		}
		result = rows[0].ErrorCategory
		if result == "" {
			result = rows[0].Status
		}
	}
	actor, _ := types.UserIDFromContext(ctx)
	row := &FeishuCollaboration{ID: uuid.NewString(), BatchID: batchID, ItemID: itemID, ActorID: actor, Action: action, Contact: strings.TrimSpace(contact), Note: strings.TrimSpace(note), Result: result, CreatedAt: time.Now()}
	if err = s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil // A report or metadata recheck never retries or publishes.
}
