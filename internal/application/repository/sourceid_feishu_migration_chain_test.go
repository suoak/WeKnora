package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/golang-migrate/migrate/v4"
	pgmigrate "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSourceIDPostgresFeishuOrderedMigrationChain(t *testing.T) {
	db := sourceIDPostgresDB(t) // Disposable schema: actual 003024 already applied.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	var schema string
	require.NoError(t, db.Raw("SELECT current_schema()").Scan(&schema).Error)
	require.NoError(t, db.Exec("CREATE TABLE knowledge_bases(id VARCHAR(36) PRIMARY KEY)").Error)
	require.NoError(t, db.Exec("CREATE TABLE system_settings(key TEXT PRIMARY KEY,value TEXT)").Error)
	user := types.User{
		ID:       uuid.NewString(),
		Username: "chain-fixture",
		Email:    "chain@example.test",
		IsActive: true,
	}
	require.NoError(t, NewUserRepository(db).CreateUser(context.Background(), &user))
	require.NoError(t, db.Exec("INSERT INTO knowledge_bases(id) VALUES (?)", "synthetic-kb").Error)
	driver, err := pgmigrate.WithInstance(sqlDB, &pgmigrate.Config{SchemaName: schema})
	require.NoError(t, err)
	m, err := migrate.NewWithDatabaseInstance("file://../../../migrations/versioned", "postgres", driver)
	require.NoError(t, err)
	defer func(cleanup func() (error, error)) { _, _ = cleanup() }(m.Close)
	// This Force establishes ONLY the fresh fixture baseline after executing
	// 003024. Never run Force on the packaging machine to skip migrations.
	require.NoError(t, m.Force(3024))
	for _, version := range []uint{3025, 3026, 3027} {
		require.NoError(t, m.Steps(1))
		v, dirty, err := m.Version()
		require.NoError(t, err)
		require.Equal(t, version, v)
		require.False(t, dirty)
	}
	for _, table := range []string{
		"feishu_connections",
		"feishu_oauth_states",
		"feishu_personal_imports",
		"source_id_binding_reviews",
		"source_id_binding_audits",
		"source_id_local_sessions",
	} {
		require.True(t, db.Migrator().HasTable(table))
	}
	var count int64
	require.NoError(t, db.Model(&types.User{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Table("knowledge_bases").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, m.Steps(-1))
	v, dirty, err := m.Version()
	require.NoError(t, err)
	require.EqualValues(t, 3026, v)
	require.False(t, dirty)
	require.True(t, db.Migrator().HasTable("feishu_connections"))
	require.True(t, db.Migrator().HasTable("source_id_binding_reviews"))
	require.NoError(t, m.Steps(-1))
	v, dirty, err = m.Version()
	require.NoError(t, err)
	require.EqualValues(t, 3025, v)
	require.False(t, dirty)
	require.True(t, db.Migrator().HasTable("feishu_connections"))
	require.NoError(t, m.Steps(-1))
	v, dirty, err = m.Version()
	require.NoError(t, err)
	require.EqualValues(t, 3024, v)
	require.False(t, dirty)
	require.NoError(t, db.Model(&types.User{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
