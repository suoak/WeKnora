package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWikiProductionConfluenceRequiresCurrentConnectorBinding(t *testing.T) {
	for _, mode := range []string{"strict", "preserving"} {
		t.Run(mode, func(t *testing.T) {
			f := fixture.New(t)
			f.PreparePublished(t)
			db := types.EnableLegacyReadGuards(f.DB)
			require.NoError(t, db.Exec(`INSERT INTO data_sources(
 id,tenant_id,knowledge_base_id,name,type,status,legacy_access_state,config)
 VALUES('confluence',1,'public-two','synthetic','confluence','active','unmanaged',
 '{"settings":{"credential_source":"system"},"resource_ids":["space"]}')`).Error)
			require.NoError(t, db.Exec(`UPDATE knowledges SET channel='confluence',parse_status='finalizing',
 metadata='{"datasource_id":"confluence","external_id":"page:42","page_id":"42","space_key":"space"}'
 WHERE id='public-two-doc'`).Error)
			repo := repository.NewWikiPageRepository(db)
			begin := func() (context.Context, error) {
				if mode == "preserving" {
					return repository.BeginWikiPreservingGeneration(context.Background(), repo, "public-two")
				}
				return repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
			}
			work, err := begin()
			require.NoError(t, err)
			gate := repository.NewModelRepository(db).(legacyModelInputGate)
			require.NoError(t, gate.CheckLegacyModelInputs(work, 1))
			page := &types.WikiPage{
				ID: "confluence-page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "summary/confluence",
				Content: "verified current source summary", PageType: "summary", Status: "published", Version: 1,
				SourceRefs: types.StringArray{"public-two-doc"},
			}
			require.NoError(t, repo.Create(work, page))
			require.NoError(t, db.Exec(`UPDATE data_sources SET last_sync_at=CURRENT_TIMESTAMP,
 updated_at=CURRENT_TIMESTAMP WHERE id='confluence'`).Error)
			require.NoError(t, gate.CheckLegacyModelInputs(work, 1), "an unchanged sync does not revoke its source")
			require.NoError(t, db.Exec(`UPDATE data_sources SET config='{"resource_ids":["changed"]}'
 WHERE id='confluence'`).Error)
			require.Error(t, gate.CheckLegacyModelInputs(work, 1), "configuration changes invalidate in-flight inputs")
			page.Content = "must not publish against old source configuration"
			require.Error(t, repo.Update(work, page))
			for _, mutation := range []string{
				`UPDATE data_sources SET status='paused' WHERE id='confluence'`,
				`UPDATE data_sources SET status='active',legacy_access_state='hold' WHERE id='confluence'`,
				`UPDATE data_sources SET legacy_access_state='revoked' WHERE id='confluence'`,
				`UPDATE data_sources SET legacy_access_state='unmanaged',type='gitlab' WHERE id='confluence'`,
				`UPDATE data_sources SET type='confluence',tenant_id=99 WHERE id='confluence'`,
				`UPDATE data_sources SET tenant_id=1,knowledge_base_id='foreign-kb' WHERE id='confluence'`,
				`UPDATE data_sources SET knowledge_base_id='public-two',deleted_at=CURRENT_TIMESTAMP
 WHERE id='confluence'`,
			} {
				require.NoError(t, db.Exec(mutation).Error)
				_, err := begin()
				require.Error(t, err, "denied, mismatched or deleted connectors cannot supply Wiki inputs")
			}
		})
	}
}
