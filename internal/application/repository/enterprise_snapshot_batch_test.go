package repository

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestEnterpriseSnapshotM7PreservesApprovalFingerprints(t *testing.T) {
	f := newEnterpriseFixture(t)
	for _, empty := range []bool{false, true} {
		if empty {
			require.NoError(t, f.db.Exec("DELETE FROM chunks").Error)
		}
		var docs []types.EnterpriseDocumentState
		require.NoError(t, f.db.Table("knowledges").Select("id,tenant_id,knowledge_base_id,type,parse_status,enable_status,channel").Find(&docs).Error)
		require.NoError(t, populateDocumentVersions(f.db, docs))
		for _, d := range docs {
			legacy, _, err := resourceVersion(f.db, types.EnterprisePolicyTarget{Kind: "knowledge", ID: d.ID})
			require.NoError(t, err)
			require.Equal(t, legacy, d.CurrentVersion, "batching must not change old approval hash format")
		}
	}
}
