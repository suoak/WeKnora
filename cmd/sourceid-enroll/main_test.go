package main

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestReviewedEnrollmentDryRunAndAtomicConflict(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(
		t,
		db.AutoMigrate(&types.User{}, &types.ExternalIdentity{}, &types.SourceIDEnrollment{}),
	)
	userID := uuid.NewString()
	require.NoError(
		t,
		db.Create(
			&types.User{ID: userID, Username: "mock", Email: "mock@example.test", IsActive: true},
		).Error,
	)
	rows, err := readRows(strings.NewReader("employee_no,user_id\n0001," + userID))
	require.NoError(t, err)
	require.NoError(t, enroll(db, rows, "issuer", "reviewer", "ticket", false))
	var count int64
	require.NoError(t, db.Model(&types.SourceIDEnrollment{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, enroll(db, rows, "issuer", "reviewer", "ticket", true))
	require.NoError(t, enroll(db, rows, "issuer", "reviewer", "ticket", true))
	require.Error(t, enroll(db, []enrollmentRow{{"0002", userID}}, "issuer", "reviewer", "ticket", true))
	newID := uuid.NewString()
	require.NoError(
		t,
		db.Create(
			&types.User{ID: newID, Username: "second", Email: "second@example.test", IsActive: true},
		).Error,
	)
	require.Error(
		t,
		enroll(
			db,
			[]enrollmentRow{{"0002", newID}, {"0003", userID}},
			"issuer",
			"reviewer",
			"ticket",
			true,
		),
	)
	require.NoError(t, db.Model(&types.SourceIDEnrollment{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.Error(t, enroll(db, rows, "issuer", "", "ticket", true))
}

func TestEnrollmentCSVRejectsDuplicatesAndEmailMatching(t *testing.T) {
	userID := uuid.NewString()
	for _, csv := range []string{
		"employee_no,email\n001,person@example.test",
		"employee_no,user_id\n001," +
			userID +
			"\n001," +
			uuid.NewString(),
		"employee_no,user_id\n001," +
			userID +
			"\n002," +
			userID,
		"employee_no,user_id\n001,invalid",
	} {
		_, err := readRows(strings.NewReader(csv))
		require.Error(t, err)
	}
}

func TestEnrollmentCSVDoesNotSilentlyAcceptTruncatedBatch(t *testing.T) {
	// Every row before the boundary is valid. A reader capped exactly at the
	// boundary would otherwise accept this prefix and miss the duplicate below.
	const limit = 1 << 20
	var b strings.Builder
	b.WriteString("employee_no,user_id\n")
	firstID := uuid.NewString()
	b.WriteString("first," + firstID + "\n")
	for b.Len()+550 < limit {
		id := uuid.NewString()
		b.WriteString(id + strings.Repeat("x", 350-len(id)) + "," + uuid.NewString() + "\n")
	}
	remaining := limit - b.Len()
	id := uuid.NewString()
	b.WriteString(id + strings.Repeat("x", remaining-38-len(id)) + "," + uuid.NewString() + "\n")
	require.Equal(t, limit, b.Len())
	b.WriteString("first," + firstID + "\n")
	_, err := readRows(strings.NewReader(b.String()))
	require.ErrorContains(t, err, "CSV exceeds size limit")
}
