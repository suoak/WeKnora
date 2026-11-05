package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWikiInputStampsReuseExactContentHashAndRejectCoveredMutation(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE chunks SET content='changed input' WHERE knowledge_id='public-two-doc'`,
		`UPDATE chunks SET is_enabled=false WHERE knowledge_id='public-two-doc'`,
		`UPDATE knowledges SET title='changed input' WHERE id='public-two-doc'`,
		`UPDATE knowledges SET enable_status='disabled' WHERE id='public-two-doc'`,
		`DELETE FROM document_access_epochs WHERE knowledge_id='public-two-doc'`,
	} {
		t.Run(mutation, func(t *testing.T) {
			f := fixture.New(t)
			f.PreparePublished(t)
			db := f.DB
			repo := repository.NewWikiPageRepository(db)
			proof, err := repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
			require.NoError(t, err)
			chunkReads := 0
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("wiki_input_reads", func(tx *gorm.DB) {
				if tx.Statement.Table == "chunks" && strings.Contains(tx.Statement.SQL.String(), "SELECT") {
					chunkReads++
				}
			}))
			page := &types.WikiPage{
				ID: "epoch-page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/epoch",
				PageType: "concept", Status: "published", Content: "synthetic page", Version: 1,
				SourceRefs: types.StringArray{"public-two-doc"},
			}
			require.NoError(t, repo.Create(proof, page))
			require.Zero(t, chunkReads, "unchanged generation must not reread every KB body per page")
			require.NoError(t, db.Exec(mutation).Error)
			page.Content = "must not publish stale content"
			require.Error(t, repo.Update(proof, page))
		})
	}
}
