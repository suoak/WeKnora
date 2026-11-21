package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NotificationRepository stores user inboxes independently of the active workspace.
type NotificationRepository struct{ db *gorm.DB }

// NewNotificationRepository binds notification writes to the supplied database or transaction.
func NewNotificationRepository(db *gorm.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// List returns one page of the recipient's notifications and its matching total.
func (r *NotificationRepository) List(
	ctx context.Context,
	userID string,
	page, size int,
	unread bool,
) ([]types.UserNotification, int64, error) {
	rows := []types.UserNotification{}
	q := r.db.WithContext(ctx).Model(&types.UserNotification{}).Where("user_id = ?", userID)
	if unread {
		q = q.Where("is_read = ?", false)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at DESC, id DESC").Limit(size).Offset((page - 1) * size).Find(&rows).Error
	return rows, total, err
}

// UnreadCount counts the recipient's unread notifications across workspaces.
func (r *NotificationRepository) UnreadCount(ctx context.Context, userID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).
		Model(&types.UserNotification{}).
		Where("user_id = ? AND is_read = ?", userID, false).
		Count(&n).
		Error
	return n, err
}

// MarkRead marks one owned notification read, or all owned notifications when id is empty.
func (r *NotificationRepository) MarkRead(ctx context.Context, userID, id string) error {
	q := r.db.WithContext(ctx).Model(&types.UserNotification{}).Where("user_id = ?", userID)
	if id != "" {
		var n int64
		if err := r.db.WithContext(ctx).Model(&types.UserNotification{}).
			Where("user_id = ? AND id = ?", userID, id).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return gorm.ErrRecordNotFound
		}
		q = q.Where("id = ?", id)
	}
	return q.Where("is_read = ?", false).Updates(map[string]any{"is_read": true, "read_at": time.Now().UTC()}).Error
}

// AccessRequestEvent must receive the same transaction as the business write.
func (r *NotificationRepository) AccessRequestEvent(
	ctx context.Context,
	request *types.TenantAccessRequest,
	event, note string,
) error {
	userIDs := []string{request.ApplicantUserID}
	if event == "access_request_submitted" {
		userIDs = []string{}
		if err := r.db.WithContext(ctx).Table("tenant_members AS tm").
			Joins("JOIN users AS u ON u.id = tm.user_id AND u.is_active = ? AND u.deleted_at IS NULL", true).
			Where("tm.tenant_id = ? AND tm.role = ? AND tm.status = ? AND tm.deleted_at IS NULL",
				request.TenantID, types.TenantRoleOwner, types.TenantMemberStatusActive).
			Distinct().Pluck("tm.user_id", &userIDs).Error; err != nil {
			return err
		}
	}
	var config struct{ DisplayName string }
	if err := r.db.WithContext(ctx).Table("tenant_portal_configs").Select("display_name").
		Where("tenant_id = ?", request.TenantID).Scan(&config).Error; err != nil {
		return err
	}
	name := config.DisplayName
	if name == "" {
		name = fmt.Sprintf("#%d", request.TenantID)
	}
	title, content := "收到新的知识空间访问申请", ""
	switch event {
	case "access_request_submitted":
		var user struct{ Username string }
		if err := r.db.WithContext(ctx).Table("users").Select("username").
			Where("id = ?", request.ApplicantUserID).Scan(&user).Error; err != nil {
			return err
		}
		if user.Username == "" {
			user.Username = request.ApplicantUserID
		}
		reason := []rune(request.Reason)
		if len(reason) > 200 {
			reason = append(reason[:200], '…')
		}
		content = fmt.Sprintf(
			"%s 申请访问「%s」\n%s\n%s",
			user.Username,
			name,
			request.CreatedAt.Format(time.RFC3339),
			string(reason),
		)
	case "access_request_approved":
		title = "知识空间访问申请已通过"
		content = "你的知识空间「" + name + "」访问申请已通过，现已获得访问权限。"
	case "access_request_rejected":
		title = "知识空间访问申请未通过"
		content = "你的知识空间「" + name + "」访问申请未通过。"
	}
	if note != "" {
		content += "\n审批意见：" + note
	}
	for _, userID := range userIDs {
		row := types.UserNotification{
			ID:               uuid.NewString(),
			UserID:           userID,
			NotificationType: event,
			Title:            title,
			Content:          content,
			ReferenceType:    "tenant_access_request",
			ReferenceID:      request.ID,
			TenantID:         request.TenantID,
			CreatedAt:        time.Now().UTC(),
		}
		if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
	}
	return nil
}
