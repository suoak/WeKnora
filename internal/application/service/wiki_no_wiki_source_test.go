package service

import (
	"context"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWikiNoWikiTerminalRequiresCurrentSourceAndPreservesHistory(t *testing.T) {
	testWikiNoWikiTerminal(t, false)
}

func TestWikiNoWikiTerminalPostgres(t *testing.T) {
	if os.Getenv("WIKI_HISTORY_POSTGRES_DSN") == "" {
		t.Skip("requires the real PostgreSQL recovery fixture")
	}
	testWikiNoWikiTerminal(t, true)
}

func testWikiNoWikiTerminal(t *testing.T, realPostgres bool) {
	t.Helper()
	for _, scenario := range []string{"unchanged", "changed_short", "longer", "image", "disabled", "missing_terminal", "wrong_scope"} {
		t.Run(scenario, func(t *testing.T) {
			f := fixture.New(t)
			if realPostgres {
				f = wikiPendingRegenerationFixture(t)
			}
			f.PreparePublished(t)
			db := types.EnableLegacyReadGuards(f.DB)
			require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}, &types.KnowledgeProcessingSpan{}, &types.WikiPageRevision{}))
			require.NoError(t, db.Model(&types.KnowledgeBase{}).Where("id='public-two'").Update("indexing_strategy", types.IndexingStrategy{WikiEnabled: true}).Error)
			require.NoError(t, db.Exec("UPDATE chunks SET content='**示例**',context_header='',parent_chunk_id='',image_info='' WHERE knowledge_id='public-two-doc'").Error)
			old := &types.WikiPage{ID: "old-short", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/historical", Title: "Historical", Content: "PRESERVE_HISTORY", PageType: "concept", Status: "published", Version: 2, SourceRefs: types.StringArray{"public-two-doc"}}
			require.NoError(t, db.Create(old).Error)
			ctx := context.Background()
			preview, err := repository.PrepareWikiPreservingRecovery(ctx, db, 1, "public-two", "", false)
			require.NoError(t, err)
			_, err = repository.PrepareWikiPreservingRecovery(ctx, db, 1, "public-two", preview.Snapshot, true)
			require.NoError(t, err)
			// Synthetic fixture consumption only; production tasks retain normal workers.
			require.NoError(t, db.Where("scope='knowledge_base' AND scope_id='public-two'").Delete(&types.TaskPendingOp{}).Error)
			repo := repository.NewWikiPageRepository(db)
			_, err = repository.BeginWikiPreservingGeneration(ctx, repo, "public-two")
			require.NoError(t, err)
			switch scenario {
			case "changed_short":
				require.NoError(t, db.Exec("UPDATE chunks SET content='**变化**' WHERE knowledge_id='public-two-doc'").Error)
			case "longer":
				require.NoError(t, db.Exec("UPDATE chunks SET content='1234567890' WHERE knowledge_id='public-two-doc'").Error)
			case "image":
				require.NoError(t, db.Exec("UPDATE chunks SET image_info='[{\"ocr_text\":\"evidence\"}]' WHERE knowledge_id='public-two-doc'").Error)
			case "disabled":
				require.NoError(t, db.Exec("UPDATE knowledges SET enable_status='disabled' WHERE id='public-two-doc'").Error)
			case "missing_terminal":
				require.NoError(t, db.Where("scope='wiki_document_skip' AND op='no_wiki'").Delete(&types.TaskPendingOp{}).Error)
			case "wrong_scope":
				require.NoError(t, db.Model(&types.TaskPendingOp{}).Where("scope='wiki_document_skip' AND op='no_wiki'").Update("tenant_id", 999).Error)
			}
			finish, err := repository.FinishWikiPreservingRecovery(ctx, db, 1, "public-two", "", false)
			if scenario != "unchanged" {
				require.Error(t, err, "old terminal cannot cover changed or inaccessible input")
				if scenario != "changed_short" {
					return
				}
				_, err = repository.BeginWikiPreservingGeneration(ctx, repo, "public-two")
				require.NoError(t, err)
				finish, err = repository.FinishWikiPreservingRecovery(ctx, db, 1, "public-two", "", false)
			}
			require.NoError(t, err)
			require.Equal(t, []string{"public-two-doc"}, finish.NoWikiDocuments)
			require.Zero(t, finish.RetainedPages, "never invent a summary for insufficient input")
			_, err = repository.FinishWikiPreservingRecovery(ctx, db, 1, "public-two", finish.Snapshot, true)
			require.NoError(t, err)
			var retained types.WikiPage
			require.NoError(t, db.Unscoped().First(&retained, "id=?", old.ID).Error)
			require.Equal(t, "PRESERVE_HISTORY", retained.Content)
			require.True(t, retained.DeletedAt.Valid)
		})
	}
}
