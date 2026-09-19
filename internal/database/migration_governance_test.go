package database

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type migrationDialectPolicy struct {
	Directory               string `json:"directory"`
	Database                string `json:"database"`
	CurrentLatest           int    `json:"current_latest"`
	ExistingKnowHubBaseline int    `json:"existing_knowhub_baseline"`
	ImmutableThrough        int    `json:"immutable_through"`
	ImmutableFileCount      int    `json:"immutable_file_count"`
	ImmutableHistorySHA256  string `json:"immutable_history_sha256"`
}

type migrationInventoryItem struct {
	SourceRepo               string `json:"source_repo"`
	SourceCommit             string `json:"source_commit"`
	SourceMigrationNumber    int    `json:"source_migration_number"`
	CanonicalMigrationNumber string `json:"canonical_migration_number"`
	Dialect                  string `json:"dialect"`
	Name                     string `json:"name"`
	SourceChecksum           string `json:"source_checksum"`
	ChecksumAlgorithm        string `json:"checksum_algorithm"`
	AdoptionStrategy         string `json:"adoption_strategy"`
}

type migrationManifest struct {
	FormatVersion int `json:"format_version"`
	Execution     struct {
		RuntimeOrder            string   `json:"runtime_order"`
		ManifestRole            string   `json:"manifest_role"`
		RuntimeManifestOrdering bool     `json:"runtime_manifest_ordering"`
		Phase1ReleaseGates      []string `json:"phase1_release_gates"`
		DeferredPaths           []string `json:"deferred_paths"`
	} `json:"execution"`
	Namespaces []struct {
		Range string `json:"range"`
		Owner string `json:"owner"`
		Rule  string `json:"rule"`
	} `json:"namespaces"`
	Dialects struct {
		Versioned migrationDialectPolicy `json:"versioned"`
		SQLite    migrationDialectPolicy `json:"sqlite"`
	} `json:"dialects"`
	LogicalParity []struct {
		LogicalID  string `json:"logical_id"`
		Versioned  int    `json:"versioned"`
		SQLite     *int   `json:"sqlite"`
		SQLiteNote string `json:"sqlite_note"`
	} `json:"logical_parity"`
	LineageMapping []struct {
		Lineage               string `json:"lineage"`
		Dialect               string `json:"dialect"`
		SourceRange           string `json:"source_range"`
		GovernedIdentityRange string `json:"governed_identity_range"`
		Action                string `json:"action"`
	} `json:"lineage_mapping"`
	UpstreamInventory struct {
		Versioned []migrationInventoryItem `json:"versioned"`
		SQLite    []migrationInventoryItem `json:"sqlite"`
	} `json:"upstream_inventory"`
}

func loadMigrationManifest(t *testing.T, repoRoot string) migrationManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "migrations", "migration-map.json"))
	require.NoError(t, err)
	var manifest migrationManifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	return manifest
}

func TestMigrationGovernanceManifest(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	manifest := loadMigrationManifest(t, repoRoot)

	require.Equal(t, 1, manifest.FormatVersion)
	require.Equal(t, "numeric_filename_prefix", manifest.Execution.RuntimeOrder)
	require.Equal(t, "validation_and_provenance", manifest.Execution.ManifestRole)
	require.False(t, manifest.Execution.RuntimeManifestOrdering,
		"the current file source does not support manifest-driven runtime ordering")
	require.ElementsMatch(t,
		[]string{"fresh_install_to_latest", "existing_knowhub_to_latest"},
		manifest.Execution.Phase1ReleaseGates,
	)
	require.Equal(t, []string{"existing_upstream_to_knowhub"}, manifest.Execution.DeferredPaths)
	require.Len(t, manifest.Namespaces, 5)
	require.Equal(t,
		[]string{"000000-000999", "001000-001999", "002000-002999", "003000-899999", "900000-999999"},
		[]string{
			manifest.Namespaces[0].Range,
			manifest.Namespaces[1].Range,
			manifest.Namespaces[2].Range,
			manifest.Namespaces[3].Range,
			manifest.Namespaces[4].Range,
		},
	)
	require.Len(t, manifest.LineageMapping, 6)
	require.Equal(t, 96, manifest.Dialects.Versioned.ExistingKnowHubBaseline)
	require.Equal(t, 17, manifest.Dialects.SQLite.ExistingKnowHubBaseline)
	require.Equal(t, manifest.Dialects.Versioned.ImmutableThrough, manifest.Dialects.Versioned.ExistingKnowHubBaseline)
	require.Equal(t, manifest.Dialects.SQLite.ImmutableThrough, manifest.Dialects.SQLite.ExistingKnowHubBaseline)

	validateImmutableMigrationHistory(t, repoRoot, manifest.Dialects.Versioned)
	validateImmutableMigrationHistory(t, repoRoot, manifest.Dialects.SQLite)

	logicalIDs := make(map[string]struct{})
	for _, parity := range manifest.LogicalParity {
		require.NotEmpty(t, parity.LogicalID)
		_, duplicate := logicalIDs[parity.LogicalID]
		require.Falsef(t, duplicate, "duplicate logical parity id %s", parity.LogicalID)
		logicalIDs[parity.LogicalID] = struct{}{}
		require.Positive(t, parity.Versioned)
		requireMigrationPair(t, repoRoot, manifest.Dialects.Versioned.Directory, parity.Versioned, parity.LogicalID)
		if parity.SQLite == nil {
			require.NotEmptyf(t, parity.SQLiteNote, "%s needs a SQLite non-applicability reason", parity.LogicalID)
		} else {
			requireMigrationPair(t, repoRoot, manifest.Dialects.SQLite.Directory, *parity.SQLite, parity.LogicalID)
		}
	}

	requireSequentialInventory(t, 91, 106, "versioned", manifest.UpstreamInventory.Versioned)
	requireSequentialInventory(t, 13, 25, "sqlite", manifest.UpstreamInventory.SQLite)
}

func requireMigrationPair(t *testing.T, repoRoot, directory string, version int, logicalID string) {
	t.Helper()
	base := fmt.Sprintf("%06d_%s", version, logicalID)
	require.FileExists(t, filepath.Join(repoRoot, directory, base+".up.sql"))
	require.FileExists(t, filepath.Join(repoRoot, directory, base+".down.sql"))
}

func requireSequentialInventory(t *testing.T, first, last int, dialect string, inventory []migrationInventoryItem) {
	t.Helper()
	require.Len(t, inventory, last-first+1)
	for index, item := range inventory {
		require.Equal(t, first+index, item.SourceMigrationNumber)
		require.Equal(t, "https://github.com/Tencent/WeKnora.git", item.SourceRepo)
		require.Regexp(t, `^[0-9a-f]{40}$`, item.SourceCommit)
		require.Equal(t, dialect, item.Dialect)
		require.NotEmpty(t, item.Name)
		require.Regexp(t, `^[0-9a-f]{40}:[0-9a-f]{40}$`, item.SourceChecksum)
		require.Equal(t, "git-blob-sha1-up-down", item.ChecksumAlgorithm)
		require.Regexp(t, `^\d{6}$`, item.CanonicalMigrationNumber)
		canonical, err := strconv.Atoi(item.CanonicalMigrationNumber)
		require.NoError(t, err)
		switch item.AdoptionStrategy {
		case "manual-port":
			require.GreaterOrEqual(t, canonical, 2000)
			require.Less(t, canonical, 3000)
		case "equivalence-review-no-new-migration":
			require.Less(t, canonical, 1000)
		default:
			require.Failf(t, "unknown adoption strategy", "%s", item.AdoptionStrategy)
		}
	}
}

func validateImmutableMigrationHistory(t *testing.T, repoRoot string, policy migrationDialectPolicy) {
	t.Helper()
	dir := filepath.Join(repoRoot, policy.Directory)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	versionPattern := regexp.MustCompile(`^(\d{6})_.+\.(up|down)\.sql$`)
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := versionPattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		version, err := strconv.Atoi(match[1])
		require.NoError(t, err)
		if version <= policy.ImmutableThrough {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	require.Len(t, names, policy.ImmutableFileCount)

	aggregate := sha256.New()
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		data = []byte(strings.ReplaceAll(string(data), "\r\n", "\n"))
		fileHash := sha256.Sum256(data)
		_, _ = fmt.Fprintf(aggregate, "%s\n%s\n", name, hex.EncodeToString(fileHash[:]))
	}
	require.Equal(t, policy.ImmutableHistorySHA256, hex.EncodeToString(aggregate.Sum(nil)),
		"immutable migration history changed; add a forward migration instead of editing history")
}

func TestMigrationRunnerSupportsSparseHighVersions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "migrations", "sqlite")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	migrations := map[string]string{
		"000000_baseline.up.sql":   "CREATE TABLE governance_baseline (id INTEGER PRIMARY KEY);",
		"000000_baseline.down.sql": "DROP TABLE governance_baseline;",
		"001000_lineage.up.sql":    "CREATE TABLE governance_lineage (id INTEGER PRIMARY KEY);",
		"001000_lineage.down.sql":  "DROP TABLE governance_lineage;",
		"003000_product.up.sql":    "CREATE TABLE governance_product (id INTEGER PRIMARY KEY);",
		"003000_product.down.sql":  "DROP TABLE governance_product;",
	}
	for name, sqlText := range migrations {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(sqlText), 0o600))
	}

	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() { _ = os.Chdir(previousDir) })

	dbPath := filepath.Join(t.TempDir(), "high-sparse.db")
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: dbPath}))
	db := openSQLiteDB(t, dbPath)
	version, dirty := sqliteMigrationState(t, db)
	require.Equal(t, 3000, version)
	require.False(t, dirty)
	for _, table := range []string{"governance_baseline", "governance_lineage", "governance_product"} {
		require.True(t, sqliteTableExists(t, db, table), table)
	}
}

func TestSQLiteMigrationGovernanceFreshAndExistingKnowHubConverge(t *testing.T) {
	repoRoot := sqliteRepoRoot(t)
	manifest := loadMigrationManifest(t, repoRoot)
	policy := manifest.Dialects.SQLite

	previousDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(previousDir) })

	freshPath := filepath.Join(t.TempDir(), "fresh.db")
	require.NoError(t, os.Chdir(repoRoot))
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: freshPath}))
	freshDB := openSQLiteDB(t, freshPath)
	freshVersion, freshDirty := sqliteMigrationState(t, freshDB)
	require.Equal(t, policy.CurrentLatest, freshVersion)
	require.False(t, freshDirty)
	freshFingerprint := sqliteSchemaFingerprint(t, freshDB)

	baselineRoot := copySQLiteMigrationsThrough(t, repoRoot, fmt.Sprintf("%06d", policy.ExistingKnowHubBaseline))
	upgradePath := filepath.Join(t.TempDir(), "existing-knowhub.db")
	require.NoError(t, os.Chdir(baselineRoot))
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: upgradePath}))
	upgradeDB := openSQLiteDB(t, upgradePath)
	baselineVersion, baselineDirty := sqliteMigrationState(t, upgradeDB)
	require.Equal(t, policy.ExistingKnowHubBaseline, baselineVersion)
	require.False(t, baselineDirty)
	_, err = upgradeDB.Exec("INSERT INTO tenants (name, business) VALUES (?, ?)", "governance-sentinel", "m0-upgrade")
	require.NoError(t, err)

	require.NoError(t, os.Chdir(repoRoot))
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", MigrationOptions{SQLiteDBPath: upgradePath}))
	upgradeVersion, upgradeDirty := sqliteMigrationState(t, upgradeDB)
	require.Equal(t, policy.CurrentLatest, upgradeVersion)
	require.False(t, upgradeDirty)
	require.Equal(t, freshFingerprint, sqliteSchemaFingerprint(t, upgradeDB),
		"fresh and existing KnowHub paths must converge on the same normalized schema")

	var sentinelName string
	require.NoError(t, upgradeDB.QueryRow("SELECT name FROM tenants WHERE business = ?", "m0-upgrade").Scan(&sentinelName))
	require.Equal(t, "governance-sentinel", sentinelName)
}

func sqliteSchemaFingerprint(t *testing.T, db *sql.DB) string {
	t.Helper()
	rows, err := db.Query(`SELECT type, name, tbl_name, COALESCE(sql, '')
		FROM sqlite_master
		WHERE name NOT LIKE 'sqlite_%' AND name <> 'schema_migrations'
		ORDER BY type, name, tbl_name`)
	require.NoError(t, err)
	defer rows.Close()

	hash := sha256.New()
	for rows.Next() {
		var objectType, name, tableName, sqlText string
		require.NoError(t, rows.Scan(&objectType, &name, &tableName, &sqlText))
		normalizedSQL := strings.Join(strings.Fields(sqlText), " ")
		_, _ = fmt.Fprintf(hash, "%s\t%s\t%s\t%s\n", objectType, name, tableName, normalizedSQL)
	}
	require.NoError(t, rows.Err())
	return hex.EncodeToString(hash.Sum(nil))
}
