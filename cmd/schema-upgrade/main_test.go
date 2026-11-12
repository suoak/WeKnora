package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrationTargetRejectsStaleReleaseBeforeDatabaseAccess(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"003046_existing.up.sql", "003046_existing.down.sql",
		"003047_reconciled.up.sql", "003047_reconciled.down.sql",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1;\n"), 0o600))
	}
	require.ErrorContains(t, verifyTargetVersion(dir, 3046), "no migration started")
	require.NoError(t, verifyTargetVersion(dir, 3047))
	require.Error(t, verifyTargetVersion(t.TempDir(), 3047))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "003047_collision.up.sql"), []byte("SELECT 1;\n"), 0o600))
	require.Error(t, verifyTargetVersion(dir, 3047), "a duplicate migration source cannot be used")
}
