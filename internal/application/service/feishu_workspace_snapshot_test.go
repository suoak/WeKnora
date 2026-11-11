package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceImportedSnapshotDoesNotDependOnSourceAuthorization(t *testing.T) {
	for _, change := range []string{"disconnect", "source_configuration_removed", "importer_removed", "cloud_identity_overlap", "binding", "hash", "generation", "kb_deleted"} {
		t.Run(change, func(t *testing.T) {
			f, observed, _ := pendingExactFixture(t)
			t.Setenv("FEISHU_PERSONAL_WORKSPACE_IMPORT_SNAPSHOTS", "1")
			f.s.newClient = func(*core.Config) *core.Client {
				t.Fatal("an accepted workspace copy must not call its source")
				return nil
			}
			switch change {
			case "disconnect":
				require.NoError(t, f.db.Exec("DELETE FROM feishu_connections").Error)
				require.NoError(t, f.db.Exec("DELETE FROM feishu_personal_grant_epochs").Error)
			case "source_configuration_removed":
				require.NoError(t, f.db.Exec("DELETE FROM system_settings WHERE key=?", feishuAppSetting).Error)
			case "importer_removed":
				require.NoError(t, f.db.Exec("DELETE FROM tenant_members WHERE user_id='A'").Error)
				require.NoError(t, f.db.Exec("UPDATE users SET is_active=false WHERE id='A'").Error)
			case "cloud_identity_overlap":
				require.NoError(t, f.db.Exec(`INSERT INTO feishu_legacy_resources
(id,tenant_id,knowledge_base_id,knowledge_id,state,original_file_path,external_id)
VALUES ('other-source',1,'other-kb','other-document','revoked','local://other-file','file:real')`).Error)
			case "binding":
				require.NoError(t, f.db.Exec("UPDATE resource_bindings SET owner_id='other'").Error)
			case "hash":
				require.NoError(t, f.db.Exec("UPDATE resources SET content_hash=repeat('a',64)").Error)
			case "generation":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_resources SET generation=generation+1").Error)
			case "kb_deleted":
				require.NoError(t, f.db.Exec("UPDATE knowledge_bases SET deleted_at=now() WHERE id='kb'").Error)
			}
			preflight, err := f.p.preflightExactPersonalDocument(context.Background(), observed)
			if change == "binding" || change == "hash" || change == "generation" || change == "kb_deleted" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			owner, err := f.p.claimExactPreflight(context.Background(), preflight)
			require.NoError(t, err)
			proof, err := f.p.prepareExactPersonalDocument(context.Background(), owner)
			require.NoError(t, err)
			require.Equal(t, observed.Revision, proof.revision)
			require.NotEmpty(t, proof.file)
			require.NoError(t, f.p.mutate(context.Background(), owner, 0, nil))
		})
	}
}
