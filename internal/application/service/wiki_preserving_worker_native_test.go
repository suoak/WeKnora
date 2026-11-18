//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalWikiPreservingWorkerUsesVerifiedSourceWithoutViewerGrant(t *testing.T) {
	exerciseNativeResumeWithFormat(t, false, false, nil, nativeResumeFormatOptions{
		Format: "docx", SkipAdditionalReadMatrix: true,
		AssertPublished: func(f *personalRecoveryFixture, source *personalExactDocument, _ types.PersonalExactReceipt) {
			assertPersonalWikiGeneration(t, f, source)
			ctx := context.Background()
			operator := types.WithLegacyKBAdminMetadata(ctx, source.kb)
			require.NoError(t, f.db.WithContext(operator).Model(&types.KnowledgeBase{}).Where("id=?", source.kb).
				Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
			plan, err := repository.PrepareWikiPreservingRecovery(operator, f.db, 1, source.kb, "", false)
			require.NoError(t, err)
			_, err = repository.PrepareWikiPreservingRecovery(operator, f.db, 1, source.kb, plan.Snapshot, true)
			require.NoError(t, err)
			pages := &wikiPageService{repo: repository.NewWikiPageRepository(f.db)}
			ingest := &wikiIngestService{knowledgeSvc: f.ks, wikiService: pages}
			work, err := ingest.personalWikiProofContext(ctx, source.kb)
			require.NoError(t, err, "the real worker must choose preserving mode from the operator marker")
			_, err = f.ks.GetKnowledgeByIDOnly(ctx, source.knowledge)
			require.Error(t, err, "recovery must not grant interactive viewer access")
			gone, err := ingest.isKnowledgeGone(work, source.kb, source.knowledge)
			require.NoError(t, err)
			require.False(t, gone)
			row, err := ingest.generationKnowledge(work, source.knowledge)
			require.NoError(t, err)
			require.Equal(t, source.knowledge, row.ID)
			_, err = ingest.generationKnowledge(work, "outside-selection")
			require.Error(t, err)
			wrongTenant := context.WithValue(work, types.TenantIDContextKey, uint64(2))
			_, err = ingest.generationKnowledge(wrongTenant, source.knowledge)
			require.Error(t, err)
			tx := f.db.Begin()
			require.NoError(
				t,
				tx.Exec("UPDATE chunks SET content=content||' changed' WHERE knowledge_id=?", source.knowledge).Error,
			)
			reader := repository.NewKnowledgeRepository(tx).(interface {
				ReadWikiGenerationKnowledge(context.Context, string) (*types.Knowledge, bool, error)
			})
			row, handled, err := reader.ReadWikiGenerationKnowledge(work, source.knowledge)
			require.True(t, handled)
			require.Error(t, err)
			require.Nil(t, row)
			require.NoError(t, tx.Rollback().Error)
		},
	})
}
