package types

import "time"

// UserNotification is addressed to a user across workspaces. TenantID is
// business context, never an authorization scope for reading the inbox.
type UserNotification struct {
	ID               string     `json:"id"                gorm:"primaryKey;type:varchar(36)"`
	UserID           string     `json:"-"                 gorm:"not null;uniqueIndex:idx_notification_event,priority:1"`
	NotificationType string     `json:"notification_type" gorm:"not null;uniqueIndex:idx_notification_event,priority:2"`
	Title            string     `json:"title"`
	Content          string     `json:"content"`
	ReferenceType    string     `json:"reference_type"    gorm:"uniqueIndex:idx_notification_event,priority:3"`
	ReferenceID      string     `json:"reference_id"      gorm:"uniqueIndex:idx_notification_event,priority:4"`
	TenantID         uint64     `json:"tenant_id"`
	IsRead           bool       `json:"is_read"`
	ReadAt           *time.Time `json:"read_at"`
	CreatedAt        time.Time  `json:"created_at"`
}

// TableName returns the persistent user inbox table.
func (UserNotification) TableName() string { return "user_notifications" }
