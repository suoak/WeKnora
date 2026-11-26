package repository

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPortalAccessRequestsProjectApplicantNamesWithinTenant(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "applicants.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT, email TEXT, password_hash TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.AutoMigrate(&types.TenantAccessRequest{}))
	require.False(t, db.Migrator().HasColumn(&types.TenantAccessRequest{}, "applicant_name"), "display projection must not require a migration")
	require.NoError(t, db.Exec(`INSERT INTO users VALUES
 ('alice', 'Alice Chen', 'private-email-marker', 'private-password-marker', NULL),
 ('owner', 'Owner', 'owner-private-email', 'private-password-marker', NULL),
 ('deleted', 'Deleted private name', '', '', CURRENT_TIMESTAMP),
 ('other', 'Other tenant private name', '', '', NULL)`).Error)
	reviewer := "owner"
	for _, row := range []types.TenantAccessRequest{
		{ID: "active", TenantID: 7, ApplicantUserID: "alice", ReviewedBy: &reviewer, Status: types.TenantAccessRequestPending},
		{ID: "missing", TenantID: 7, ApplicantUserID: "missing-user", Status: types.TenantAccessRequestPending},
		{ID: "deleted", TenantID: 7, ApplicantUserID: "deleted", Status: types.TenantAccessRequestPending},
		{ID: "other", TenantID: 99, ApplicantUserID: "other", Status: types.TenantAccessRequestPending},
	} {
		require.NoError(t, db.Create(&row).Error)
	}
	rows, err := NewPortalRepository(db).ListAccessRequestsByTenant(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	byID := map[string]*types.TenantAccessRequest{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	require.Equal(t, "Alice Chen", byID["active"].ApplicantName)
	require.Equal(t, "Owner", byID["active"].ReviewerName)
	require.Equal(t, "alice", byID["active"].ApplicantUserID)
	require.Empty(t, byID["missing"].ApplicantName)
	require.Empty(t, byID["deleted"].ApplicantName)
	payload, err := json.Marshal(rows)
	require.NoError(t, err)
	for _, private := range []string{"private-email-marker", "private-password-marker", "Deleted private name", "Other tenant private name", "password_hash", `"email"`} {
		require.NotContains(t, string(payload), private)
	}
}
