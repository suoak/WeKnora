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

func wikiReadSessionFixture(
	t *testing.T,
) (*fixture.Fixture, *repository.PersonalWikiStore, context.Context, []*types.WikiPage, *int) {
	t.Helper()
	f := fixture.New(t)
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
