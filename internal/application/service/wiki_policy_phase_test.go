package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type wikiPolicyPhaseObserver struct {
	*repository.PersonalWikiStore
	t              *testing.T
	loads          int
	afterCandidate func()
}

func (w *wikiPolicyPhaseObserver) snapshot(ctx context.Context) {
	w.t.Helper()
	_, err := access.ReadPortalSnapshot(ctx, w, "A", 1, "public-two", true, true,
		func() (*types.EnterpriseReadSnapshot, error) {
			w.loads++
			return &types.EnterpriseReadSnapshot{}, nil
		})
	require.NoError(w.t, err)
}

func (w *wikiPolicyPhaseObserver) VerifiedSearchPages(
	ctx context.Context, tenant uint64, kb string, ids []string, query string,
) ([]*types.WikiPage, bool, error) {
	w.snapshot(ctx)
	w.snapshot(ctx)
	require.Equal(w.t, 1, w.loads, "repeated policy reads share one candidate phase")
	pages, partial, err := w.PersonalWikiStore.VerifiedSearchPages(ctx, tenant, kb, ids, query)
	if w.afterCandidate != nil {
		w.afterCandidate()
	}
	return pages, partial, err
}

func (w *wikiPolicyPhaseObserver) RevalidatePages(
	ctx context.Context, tenant uint64, kb string, ids []string, pages []*types.WikiPage,
) error {
	w.snapshot(ctx)
	require.Equal(w.t, 2, w.loads, "the output fence must not reuse candidate policy facts")
	return w.PersonalWikiStore.RevalidatePages(ctx, tenant, kb, ids, pages)
}

func TestWikiCandidatePolicyPhaseKeepsFreshOutputAuthorization(t *testing.T) {
	for name, mutation := range map[string]string{
		"unchanged": "",
		"key":       "UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1",
		"portal":    "UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=1",
		"document":  "UPDATE knowledges SET enable_status='disabled' WHERE id='public-two-doc'",
		"source": "INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,config," +
			"legacy_access_state,status) VALUES('held',1,'public-two','held','feishu','{}','hold','active')",
	} {
		t.Run(name, func(t *testing.T) {
			f, store, _, _, _ := wikiReadSessionFixture(t)
			s := publishedServiceM7(t, f)
			observer := &wikiPolicyPhaseObserver{PersonalWikiStore: store, t: t}
			if mutation != "" {
				observer.afterCandidate = func() { require.NoError(t, f.DB.Exec(mutation).Error) }
			}
			s.Wiki = observer
			result, err := s.PersonalWiki(fixture.Key("A", 1, 1, true), "wiki_search",
				"verified", "public-two", "", 10, 0)
			if mutation == "" {
				require.NoError(t, err)
				require.Len(t, result.Pages, 1)
				require.Equal(t, 2, observer.loads)
			} else {
				require.Error(t, err, "a cached candidate must not survive a live revocation")
				require.Nil(t, result)
			}
		})
	}
}

func TestWikiRepositoryOutputFenceRetainsLiveSourceAuthority(t *testing.T) {
	f, store, _, _, _ := wikiReadSessionFixture(t)
	_ = publishedServiceM7(t, f)
	ctx := repository.WithWikiReadProofSession(access.WithPortalSnapshotReads(fixture.Key("A", 1, 1, true)))
	pages, _, err := store.VerifiedPages(ctx, 1, "public-two", []string{"public-two-doc"})
	require.NoError(t, err)
	require.Len(t, pages, 1)
	require.NoError(t, f.DB.Exec(`INSERT INTO data_sources
(id,tenant_id,knowledge_base_id,name,type,config,legacy_access_state,status)
VALUES('output-held',1,'public-two','held','feishu','{}','hold','active')`).Error)
	require.Error(t, store.RevalidatePages(ctx, 1, "public-two", []string{"public-two-doc"}, pages))
}
