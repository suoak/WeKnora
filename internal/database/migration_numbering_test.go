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
	assertMigrationNumbers(t, filepath.Join(repoRoot, "migrations", "versioned"), "000095")
	assertMigrationNumbers(t, filepath.Join(repoRoot, "migrations", "sqlite"), "000016")
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

func assertMigrationNumbers(t *testing.T, dir, expectedLatest string) {
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
	require.Equal(t, expectedLatest, latest)
	latestNumber, err := strconv.Atoi(expectedLatest)
	require.NoError(t, err)
	for version := 0; version <= latestNumber; version++ {
		_, ok := versions[fmt.Sprintf("%06d", version)]
		require.Truef(t, ok, "missing migration version %06d in %s", version, dir)
	}
}
