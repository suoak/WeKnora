// Package feishulegacy admits only explicitly configured disposable databases.
// It creates a fresh database per test invocation, never mutates a shared app
// schema, and leaves formal migration execution to the actual project migrator.
package feishulegacy

import (
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
)

func PrivateDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("FEISHU_LEGACY_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable feishu_atomic_test PostgreSQL/ParadeDB")
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "/feishu_atomic_test", parsed.Path, "never admit a shared or production database")
	admin, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer admin.Close()
	name := "feishu_atomic_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec("CREATE DATABASE " + name)
	require.NoError(t, err)
	parsed.Path = "/" + name
	return parsed.String()
}
