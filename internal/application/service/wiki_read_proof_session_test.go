package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func wikiReadSessionFixture(
	t *testing.T,
) (*fixture.Fixture, *repository.PersonalWikiStore, context.Context, []*types.WikiPage, *int) {
	t.Helper()
	return wikiReadSessionWithFixture(t, fixture.New(t))
}

func wikiReadSessionWithFixture(
	t *testing.T, f *fixture.Fixture,
) (*fixture.Fixture, *repository.PersonalWikiStore, context.Context, []*types.WikiPage, *int) {
	t.Helper()
	f.PreparePublished(t)
	r := repository.NewWikiPageRepository(f.DB)
	gen, err := repository.BeginWikiMCPGeneration(context.Background(), r, "public-two")
	require.NoError(t, err)
	page := &types.WikiPage{
		ID:              "session-page",
		TenantID:        1,
		KnowledgeBaseID: "public-two",
		Slug:            "concept/session",
		Title:           "session",
		Content:         "verified source body",
		Status:          types.WikiPageStatusPublished,
		PageType:        types.WikiPageTypeConcept,
		SourceRefs:      types.StringArray{"public-two-doc|source"},
		Version:         1,
	}
	require.NoError(t, r.Create(gen, page))
	reads := 0
	require.NoError(
		t,
		f.DB.Callback().Query().Before("gorm:query").Register("read_session_full_input", func(tx *gorm.DB) {
			if tx.Statement.Table == "chunks" &&
				strings.Contains(strings.Join(tx.Statement.Selects, ","), "image_info") {
				reads++
			}
		}),
	)
	ctx := repository.WithWikiReadProofSession(fixture.Key("A", 1, 1, true))
	store := &repository.PersonalWikiStore{DB: f.DB}
	pages, _, err := store.VerifiedPages(ctx, 1, "public-two", []string{"public-two-doc"})
	require.NoError(t, err)
	require.Len(t, pages, 1)
	return f, store, ctx, pages, &reads
}

func TestWikiReadProofSessionRevalidatesFreshEpochsWithoutRereadingBodies(t *testing.T) {
	f, store, ctx, pages, reads := wikiReadSessionFixture(t)
	require.Equal(t, 1, *reads)
	require.NoError(t, store.RevalidatePages(ctx, 1, "public-two", []string{"public-two-doc"}, pages))
	require.Equal(
		t,
		1,
		*reads,
		"the output boundary should revalidate live stamps and source state, not reread unchanged input bodies",
	)
	require.NoError(
		t,
		f.DB.Exec("UPDATE chunks SET content='changed after first read' WHERE knowledge_id='public-two-doc'").Error,
	)
	require.Error(t, store.RevalidatePages(ctx, 1, "public-two", []string{"public-two-doc"}, pages))
	require.Greater(t, *reads, 1, "changed trigger-backed inputs must invalidate the proof-local digest")
}

func TestWikiReadProofSessionDoesNotCacheConnectorAuthority(t *testing.T) {
	f, store, ctx, pages, _ := wikiReadSessionFixture(t)
	require.NoError(t, f.DB.Exec(`INSERT INTO data_sources
(id,tenant_id,knowledge_base_id,name,type,config,legacy_access_state,status)
VALUES('held-session-source',1,'public-two','held source','feishu','{}','hold','active')`).Error)
	require.Error(
		t,
		store.RevalidatePages(ctx, 1, "public-two", []string{"public-two-doc"}, pages),
		"an unchanged document epoch must not bypass a newly held source",
	)
}

func TestWikiReadSelectedProofSessionKeepsLiveSourceAndInputFences(t *testing.T) {
	testWikiReadSelectedProofSession(t, fixture.New)
}

func TestWikiReadSelectedProofSessionPostgres(t *testing.T) {
	if os.Getenv("WIKI_HISTORY_POSTGRES_DSN") == "" {
		t.Skip("requires isolated migrated PostgreSQL")
	}
	testWikiReadSelectedProofSession(t, wikiPendingRegenerationFixture)
}

func testWikiReadSelectedProofSession(t *testing.T, newFixture func(*testing.T) *fixture.Fixture) {
	t.Helper()
	for _, mutation := range []string{"none", "content", "disable", "source"} {
		t.Run(mutation, func(t *testing.T) {
			t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION", "true")
			t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_KBS", "public-two")
			t.Setenv("KNOWHUB_WIKI_DOCUMENT_SELECTION_ACTIVE_FILE", "")
			f, store, ctx, pages, reads := wikiReadSessionWithFixture(t, newFixture(t))
			require.Equal(t, 1, *reads)
			var statement string
			switch mutation {
			case "content":
				statement = "UPDATE chunks SET content='selected raw change' WHERE knowledge_id='public-two-doc'"
			case "disable":
				statement = "UPDATE knowledges SET enable_status='disabled' WHERE id='public-two-doc'"
			case "source":
				statement = "UPDATE knowledges SET channel='unsupported-source' WHERE id='public-two-doc'"
			}
			if statement != "" {
				require.NoError(t, f.DB.Exec(statement).Error)
			}
			err := store.RevalidatePages(ctx, 1, "public-two", []string{"public-two-doc"}, pages)
			if mutation == "none" {
				require.NoError(t, err)
				require.Equal(t, 1, *reads, "selected producer inputs must not be rehashed at an unchanged output boundary")
				fresh := repository.WithWikiReadProofSession(fixture.Key("A", 1, 1, true))
				require.NoError(t, store.RevalidatePages(fresh, 1, "public-two", []string{"public-two-doc"}, pages))
				require.Greater(t, *reads, 1, "proof digests never cross requests")
			} else {
				require.Error(t, err)
				if mutation == "content" {
					require.Greater(t, *reads, 1, "raw input mutations invalidate the proof digest")
				}
			}
		})
	}
}

func TestWikiReadSealPrefilterRejectsUnreturnablePagesWithoutSourceBodyReads(t *testing.T) {
	for _, mutation := range []string{
		"DELETE FROM wiki_mcp_provenance WHERE page_id='session-page'",
		"UPDATE wiki_pages SET content='unsealed raw edit' WHERE id='session-page'",
		"UPDATE wiki_mcp_provenance SET source_versions='{}' WHERE page_id='session-page'",
	} {
		t.Run(mutation, func(t *testing.T) {
			f, store, _, _, reads := wikiReadSessionFixture(t)
			require.NoError(t, f.DB.Exec(mutation).Error)
			*reads = 0
			ctx := repository.WithWikiReadProofSession(fixture.Key("A", 1, 1, true))
			pages, partial, err := store.VerifiedPages(ctx, 1, "public-two", []string{"public-two-doc"})
			require.NoError(t, err)
			require.Empty(t, pages)
			require.True(t, partial)
			require.Zero(t, *reads, "rejected page seals must not trigger full KB body hashing")
		})
	}
}

func TestWikiReadSealPrefilterStillChecksCurrentSourceContent(t *testing.T) {
	f, store, _, _, reads := wikiReadSessionFixture(t)
	require.NoError(
		t,
		f.DB.Exec("UPDATE chunks SET content='new source body' WHERE knowledge_id='public-two-doc'").Error,
	)
	*reads = 0
	ctx := repository.WithWikiReadProofSession(fixture.Key("A", 1, 1, true))
	pages, partial, err := store.VerifiedPages(ctx, 1, "public-two", []string{"public-two-doc"})
	require.NoError(t, err)
	require.Empty(t, pages)
	require.True(t, partial)
	require.Greater(t, *reads, 0, "a valid persisted seal must still check live content")
}

func TestWikiReadInputBatchPreservesHashesAndRejectsSourceMutation(t *testing.T) {
	f, store, _, _, _ := wikiReadSessionFixture(t)
	// Existing seal was generated through the same complete canonical digest.
	// A fresh operation must return the same published body, then reject a raw edit.
	ctx := repository.WithWikiReadProofSession(fixture.Key("A", 1, 1, true))
	pages, _, err := store.VerifiedPages(ctx, 1, "public-two", []string{"public-two-doc"})
	require.NoError(t, err)
	require.Len(t, pages, 1)
	require.Equal(t, "verified source body", pages[0].Content)
	require.NoError(
		t,
		f.DB.Exec("UPDATE chunks SET content='raw batch mutation' WHERE knowledge_id='public-two-doc'").Error,
	)
	require.Error(t, store.RevalidatePages(ctx, 1, "public-two", []string{"public-two-doc"}, pages))
}
