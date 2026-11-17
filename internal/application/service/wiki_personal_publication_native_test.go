//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPersonalPublishedWikiInputRequiresCurrentCompleteReceipt(t *testing.T) {
	exerciseNativeResumeWithFormat(t, false, false, nil, nativeResumeFormatOptions{
		Format: "docx",
		AssertPublished: func(
			f *personalRecoveryFixture, source *personalExactDocument, proof types.PersonalExactReceipt,
		) {
			db := f.db.WithContext(context.Background())
			digest, err := repository.WikiPersonalPublication(db, 1, source.kb, source.knowledge)
			require.NoError(t, err)
			require.Len(t, digest, 64)
			for _, fault := range []string{
				"staging receipt", "wrong tenant", "changed binding", "disabled file",
				"changed chunk", "changed vector", "missing receipt",
			} {
				tx := db.Begin()
				require.NoError(t, tx.Error)
				tenant := uint64(1)
				switch fault {
				case "staging receipt":
					require.NoError(t, tx.Model(&types.TaskPendingOp{}).Where(
						"scope=? AND scope_id=?", types.PersonalExactReceiptScope, source.knowledge,
					).Update("op", "staging").Error)
				case "wrong tenant":
					tenant = 2
				case "changed binding":
					require.NoError(t, tx.Model(&types.ResourceBinding{}).Where("id=?", proof.BindingID).
						Update("owner_id", "other-document").Error)
				case "disabled file":
					require.NoError(t, tx.Model(&types.StoredResource{}).Where("id=?", proof.ResourceID).
						Update("state", "inactive").Error)
				case "changed chunk":
					require.NoError(t, tx.Exec("UPDATE chunks SET content=content||' changed' WHERE knowledge_id=?",
						source.knowledge).Error)
				case "changed vector":
					require.NoError(t, tx.Exec("UPDATE embeddings SET content=content||' changed' WHERE knowledge_id=?",
						source.knowledge).Error)
				case "missing receipt":
					require.NoError(t, tx.Where("scope=? AND scope_id=?",
						types.PersonalExactReceiptScope, source.knowledge).Delete(&types.TaskPendingOp{}).Error)
				}
				_, err := repository.WikiPersonalPublication(tx, tenant, source.kb, source.knowledge)
				require.ErrorIs(t, err, repository.ErrWikiMCPUnverified, fault)
				require.NoError(t, tx.Rollback().Error)
			}
			again, err := repository.WikiPersonalPublication(db, 1, source.kb, source.knowledge)
			require.NoError(t, err)
			require.Equal(t, digest, again)
			assertPersonalWikiGeneration(t, f, source)
		},
	})
}

func assertPersonalWikiGeneration(t *testing.T, f *personalRecoveryFixture, source *personalExactDocument) {
	t.Helper()
	db := f.db.WithContext(context.Background())
	require.NoError(t, db.AutoMigrate(&types.KnowledgeBase{}, &types.WikiPage{}))
	require.NoError(t, db.Table("knowledge_bases").Where("id=?", source.kb).
		Updates(map[string]any{"type": "document", "is_temporary": false}).Error)
	for _, migration := range []string{"003035_document_access", "003037_personal_dynamic_wiki"} {
		ddl, err := os.ReadFile("../../../migrations/versioned/" + migration + ".up.sql")
		require.NoError(t, err)
		text := string(ddl)
		if migration == "003037_personal_dynamic_wiki" {
			text = text[strings.Index(text, "CREATE TABLE wiki_mcp_provenance"):]
		}
		require.NoError(t, db.Exec(text).Error)
	}
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION", "true")
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_KBS", source.kb)
	t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_ACTIVE_FILE", "")
	preview, err := repository.PreviewWikiDocumentSelection(context.Background(), db, 1, source.kb)
	require.NoError(t, err)
	require.Contains(t, preview.EligibleDocuments, source.knowledge)
	repo := repository.NewWikiPageRepository(db)
	work, selection, err := repository.BeginWikiDocumentGeneration(context.Background(), repo, source.kb)
	require.NoError(t, err)
	require.True(t, selection.Eligible[source.knowledge])
	var chunks []*types.Chunk
	require.NoError(t, db.WithContext(work).Where("knowledge_id=?", source.knowledge).Find(&chunks).Error)
	require.NotEmpty(t, chunks, "the normal verified Wiki guard permits the published personal body")
	modelGate := repository.NewModelRepository(db).(legacyModelInputGate)
	require.NoError(t, modelGate.CheckLegacyModelInputs(work, 1),
		"the live model boundary must independently revalidate the same selected personal inputs")
	page := &types.WikiPage{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: source.kb, Slug: "verified-personal-source",
		Title: "Published local source", Content: "Summary generated from the checked local document.",
		PageType: types.WikiPageTypeSummary, Status: types.WikiPageStatusPublished,
		SourceRefs: types.StringArray{source.knowledge},
	}
	require.NoError(t, repo.Create(work, page), "new Wiki output must be sealed against the personal receipt")
	var sealed int64
	require.NoError(t, db.Table("wiki_mcp_provenance").Where("page_id=?", page.ID).Count(&sealed).Error)
	require.EqualValues(t, 1, sealed)
	// Timestamp-preserving tampering invalidates the live document proof. The
	// guard rejects subsequent body/model use; an old generation cannot write.
	tx := db.Begin()
	require.NoError(t, tx.Exec("UPDATE chunks SET content=content||' changed' WHERE knowledge_id=?",
		source.knowledge).Error)
	var invalid []*types.Chunk
	require.Error(t, tx.WithContext(work).Where("knowledge_id=?", source.knowledge).Find(&invalid).Error)
	require.Error(t, repository.NewModelRepository(tx).(legacyModelInputGate).CheckLegacyModelInputs(work, 1))
	require.NoError(t, tx.Rollback().Error)
}
