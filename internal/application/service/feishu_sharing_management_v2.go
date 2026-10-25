package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// ListSharingApprovals returns the owner's retained publication decisions.
func (s *FeishuPersonalService) ListSharingApprovals(
	ctx context.Context, kbID string, offset int,
) ([]types.FeishuSharingApproval, error) {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return nil, access.ErrUnauthorized
	}
	if offset < 0 || offset > 10000 {
		return nil, errors.New("invalid_approval_history_offset")
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return nil, err
	}
	rows := []types.FeishuSharingApproval{}
	err = s.db.WithContext(ctx).Where("owner_id = ? AND kb_id = ?", user, kbID).
		Order("created_at DESC, id DESC").Offset(offset).Limit(20).Find(&rows).Error
	return rows, err
}

// RevokeSharingApproval only restricts future admission/publication. It does not
// delete already published knowledge or change Feishu permissions. No live
// connection is required to withdraw an approval, including after disconnect.
func (s *FeishuPersonalService) RevokeSharingApproval(ctx context.Context, kbID, id string) error {
	user, ok := types.UserIDFromContext(ctx)
	if !ok {
		return access.ErrUnauthorized
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return err
	}
	// The write lock conflicts with the publication guard's SHARE lock, so a
	// completed revocation cannot be overtaken by a previously checked grant.
	change := s.db.WithContext(ctx).Model(&types.FeishuSharingApproval{}).
		Where("id = ? AND owner_id = ? AND kb_id = ?", id, user, kbID).
		Update("state", "revoked")
	if change.Error != nil {
		return change.Error
	}
	if change.RowsAffected == 0 {
		return access.ErrNotFound
	}
	return nil
}
