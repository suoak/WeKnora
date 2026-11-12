package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWikiDocumentSelectionRuntimeActivationStaysWithinReviewedKBs(t *testing.T) {
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION", "true")
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_KBS", "canary,reviewed")
	path := filepath.Join(t.TempDir(), "active.json")
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_ACTIVE_FILE", path)
	require.False(t, WikiDocumentSelectionForKB("canary"), "missing rollout file leaves generation unchanged")
	require.NoError(t, os.WriteFile(path, []byte(`["canary","unreviewed"]`), 0o600))
	require.True(t, WikiDocumentSelectionForKB("canary"))
	require.False(t, WikiDocumentSelectionForKB("reviewed"))
	require.False(
		t,
		WikiDocumentSelectionForKB("unreviewed"),
		"runtime activation cannot expand the reviewed upper bound",
	)
	require.NoError(t, os.WriteFile(path, []byte(`["canary","reviewed"]`), 0o600))
	require.True(
		t,
		WikiDocumentSelectionForKB("reviewed"),
		"operator can expand within the reviewed set without another APP restart",
	)
	require.NoError(t, os.WriteFile(path, []byte(`{"canary":true}`), 0o600))
	require.False(t, WikiDocumentSelectionForKB("canary"))
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_ACTIVE_FILE", "")
	require.True(
		t,
		WikiDocumentSelectionForKB("canary"),
		"existing opt-in environments retain their direct allowlist behavior",
	)
}
