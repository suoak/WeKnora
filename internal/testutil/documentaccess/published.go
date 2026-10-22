package documentaccess

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

// PreparePublished uses only synthetic records. Existing approval fixtures are
// removed to prove that the new product mode does not require/backfill them.
// Sensitive public exceptions remain whole-KB settings, not a document-ACL claim.
func (f *Fixture) PreparePublished(t *testing.T) {
	t.Helper()
	f.Portal(t)
	for _, table := range []string{"knowledge_access_policies", "kb_publication_policies", "enterprise_space_policies"} {
		require.NoError(t, f.DB.Exec("DELETE FROM "+table).Error)
	}
	for _, kb := range []string{"private-home", "private-kb", "unknown-kb"} {
		tenant := uint64(99)
		if kb == "private-home" {
			tenant = 1
		}
		require.NoError(t, f.Store.SetPortalKBPublicBlock(context.Background(), tenant, kb, "owner", "synthetic whole-KB public exception", 0, true))
	}
}
