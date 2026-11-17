//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalWikiWorkerReadsOnlyVerifiedGeneration(t *testing.T) {
	exerciseNativeResumeWithFormat(t, false, false, nil, nativeResumeFormatOptions{
		Format: "docx", SkipAdditionalReadMatrix: true,
		AssertPublished: func(f *personalRecoveryFixture, source *personalExactDocument, _ types.PersonalExactReceipt) {
			assertPersonalWikiGeneration(t, f, source)
			ctx := context.Background()
			work, _, err := repository.BeginWikiDocumentGeneration(ctx, repository.NewWikiPageRepository(f.db), source.kb)
			require.NoError(t, err)
			ingest := &wikiIngestService{knowledgeSvc: f.ks}
			_, err = f.ks.GetKnowledgeByIDOnly(ctx, source.knowledge)
			require.Error(t, err, "background work has no interactive viewer authority")
			gone, err := ingest.isKnowledgeGone(work, source.kb, source.knowledge)
			require.NoError(t, err, "use the real worker existence path, not only direct chunk queries")
			require.False(t, gone)
			row, err := ingest.generationKnowledge(work, source.knowledge)
			require.NoError(t, err)
			require.Equal(t, source.knowledge, row.ID)
			require.NotEmpty(t, row.Title)
			_, err = ingest.generationKnowledge(work, "outside-selection")
			require.Error(t, err)
			wrongTenant := context.WithValue(work, types.TenantIDContextKey, uint64(2))
			_, err = ingest.generationKnowledge(wrongTenant, source.knowledge)
			require.Error(t, err)
			tx := f.db.Begin()
			require.NoError(t, tx.Exec("UPDATE chunks SET content=content||' changed' WHERE knowledge_id=?",
				source.knowledge).Error)
			reader := repository.NewKnowledgeRepository(tx).(interface {
				ReadWikiGenerationKnowledge(context.Context, string) (*types.Knowledge, bool, error)
			})
			row, handled, err := reader.ReadWikiGenerationKnowledge(work, source.knowledge)
			require.True(t, handled)
			require.Error(t, err)
			require.Nil(t, row, "a stale generation returns no metadata or body")
			require.NoError(t, tx.Rollback().Error)
		},
	})
}

func TestPersonalPublishedFolderMovesPreserveBodyProof(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "new-publication", true: "legacy-witness"}[legacy], func(t *testing.T) {
			exerciseNativeResumeWithFormat(t, false, false, nil, nativeResumeFormatOptions{
				Format: "docx", SkipAdditionalReadMatrix: true,
				AssertPublished: func(f *personalRecoveryFixture, source *personalExactDocument, published types.PersonalExactReceipt) {
					assertPersonalWikiGeneration(t, f, source)
					ddl, err := os.ReadFile("../../../migrations/versioned/003048_feishu_folder_placement.up.sql")
					require.NoError(t, err)
					require.NoError(t, f.db.Exec(string(ddl)).Error)
					if legacy {
						require.NoError(t, f.db.Exec("UPDATE task_pending_ops SET payload=payload::jsonb-"+
							"'knowledge_folder_path' WHERE scope=? AND scope_id=?",
							types.PersonalExactReceiptScope, source.knowledge).Error)
						published.KnowledgeFolderPath = nil
					}
					ctx := context.Background()
					before, err := repository.WikiPersonalPublication(f.db, 1, source.kb, source.knowledge)
					require.NoError(t, err)
					_, err = repository.NewKnowledgeRepository(f.db).UpdateKnowledgeFolderPath(ctx, 1,
						source.kb, []string{source.knowledge}, "CLOUD")
					require.NoError(t, err)
					if legacy {
						_, err = repository.WikiPersonalPublication(f.db, 1, source.kb, source.knowledge)
						require.Error(t, err, "old receipts remain strict until their original folder is proved")
						_, err = repository.PrepareWikiPersonalFolderProof(ctx, f.db, 1, source.kb,
							source.knowledge, "wrong", "", false)
						require.Error(t, err)
						plan, err := repository.PrepareWikiPersonalFolderProof(ctx, f.db, 1, source.kb,
							source.knowledge, "", "", false)
						require.NoError(t, err)
						_, err = repository.PrepareWikiPersonalFolderProof(ctx, f.db, 1, source.kb,
							source.knowledge, "", "stale", true)
						require.Error(t, err)
						applied, err := repository.PrepareWikiPersonalFolderProof(ctx, f.db, 1, source.kb,
							source.knowledge, "", plan.Snapshot, true)
						require.NoError(t, err)
						require.True(t, applied.Applied)
						empty := ""
						published.KnowledgeFolderPath = &empty
					}
					after, err := repository.WikiPersonalPublication(f.db, 1, source.kb, source.knowledge)
					require.NoError(t, err)
					require.Equal(t, before, after, "folder changes preserve existing Wiki provenance")
					require.NoError(t, f.p.verifyExactPublishedDatabase(ctx, published))
					var folder string
					require.NoError(t, f.db.Table("knowledges").Select("folder_path").
						Where("id=?", source.knowledge).Scan(&folder).Error)
					require.Equal(t, "CLOUD", folder)
					tx := f.db.Begin()
					require.NoError(t, tx.Exec("UPDATE chunks SET content=content||' changed' WHERE knowledge_id=?",
						source.knowledge).Error)
					_, err = repository.WikiPersonalPublication(tx, 1, source.kb, source.knowledge)
					require.Error(t, err, "folder metadata never excuses changed content")
					require.NoError(t, tx.Rollback().Error)
				},
			})
		})
	}
}
