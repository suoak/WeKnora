package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrationNumbersAreUniqueAndPaired(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	manifest := loadMigrationManifest(t, repoRoot)
	assertMigrationNumbers(t, filepath.Join(repoRoot, manifest.Dialects.Versioned.Directory), manifest.Dialects.Versioned)
	assertMigrationNumbers(t, filepath.Join(repoRoot, manifest.Dialects.SQLite.Directory), manifest.Dialects.SQLite)
}

func TestMCPToolEnabledFollowsUserMCPAPIKeys(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	require.FileExists(t, filepath.Join(repoRoot, "migrations", "versioned", "000091_user_mcp_api_keys.up.sql"))
	require.FileExists(t, filepath.Join(repoRoot, "migrations", "versioned", "000091_user_mcp_api_keys.down.sql"))
	require.FileExists(t, filepath.Join(repoRoot, "migrations", "versioned", "000092_mcp_tool_enabled.up.sql"))
	require.FileExists(t, filepath.Join(repoRoot, "migrations", "versioned", "000092_mcp_tool_enabled.down.sql"))

	require.FileExists(t, filepath.Join(repoRoot, "migrations", "sqlite", "000013_user_mcp_api_keys.up.sql"))
	require.FileExists(t, filepath.Join(repoRoot, "migrations", "sqlite", "000013_user_mcp_api_keys.down.sql"))
	require.FileExists(t, filepath.Join(repoRoot, "migrations", "sqlite", "000014_mcp_tool_enabled.up.sql"))
	require.FileExists(t, filepath.Join(repoRoot, "migrations", "sqlite", "000014_mcp_tool_enabled.down.sql"))
}

func assertMigrationNumbers(t *testing.T, dir string, policy migrationDialectPolicy) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	type pair struct{ up, down string }
	versions := make(map[string]pair)
	latest := ""
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) < 8 || (filepath.Ext(name) != ".sql") {
			continue
		}
		version := name[:6]
		migration := versions[version]
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			require.Emptyf(t, migration.up, "duplicate up migration version %s: %s and %s", version, migration.up, name)
			migration.up = name
		case strings.HasSuffix(name, ".down.sql"):
			require.Emptyf(t, migration.down, "duplicate down migration version %s: %s and %s", version, migration.down, name)
			migration.down = name
		default:
			continue
		}
		versions[version] = migration
		if version > latest {
			latest = version
		}
	}

	for version, migration := range versions {
		require.NotEmptyf(t, migration.up, "migration %s has no up file", version)
		require.NotEmptyf(t, migration.down, "migration %s has no down file", version)
		require.Equalf(t,
			strings.TrimSuffix(migration.up, ".up.sql"),
			strings.TrimSuffix(migration.down, ".down.sql"),
			"migration %s up/down names do not match", version,
		)
	}
	require.Equal(t, fmt.Sprintf("%06d", policy.CurrentLatest), latest)
	for version := 0; version <= policy.ImmutableThrough; version++ {
		_, ok := versions[fmt.Sprintf("%06d", version)]
		require.Truef(t, ok, "missing immutable historical migration version %06d in %s", version, dir)
	}
	for version := range versions {
		n, err := strconv.Atoi(version)
		require.NoError(t, err)
		if n > policy.ImmutableThrough {
			require.GreaterOrEqualf(t, n, 1000, "future migration %s must use a governed high-number namespace", version)
		}
	}
}
