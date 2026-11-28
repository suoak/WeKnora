package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPortalWikiVisibleLinkLabelsAndMarkdownStructure(t *testing.T) {
	visible := map[string]*types.WikiPage{
		"concept/dpd": {Slug: "concept/dpd", Title: "链路探测 DPD"},
		"concept/点到点": {Slug: "concept/点到点", Title: "点到点"},
	}
	for _, tc := range []struct{ text, want string }{
		{"开启 [[concept/dpd|DPD]]。", "开启 DPD。"},
		{"默认 [[concept/点到点]]。", "默认 点到点。"},
		{`| [[concept/dpd\|DPD]] |`, "| DPD |"},
		{"[[missing|敏感名称]] [[other-space:concept/dpd|跨空间名称]]", " "},
		{"[[../concept/dpd|越界名称]]", ""},
		{"`[[missing|literal]]`", "`[[missing|literal]]`"},
		{"```xml\n<tag>[[missing|literal]]</tag>\n```", "```xml\n<tag>[[missing|literal]]</tag>\n```"},
		{"    [[missing|literal]]\n", "    [[missing|literal]]\n"},
		{`\[[missing|literal]]`, `\[[missing|literal]]`},
		{"<tag>协议</tag>\n**参数** [[concept/dpd|DPD]]", "<tag>协议</tag>\n**参数** DPD"},
	} {
		require.Equal(t, tc.want, renderVisibleWikiLinks(tc.text, visible), tc.text)
	}
}

type wikiLinkOutputObserver struct {
	*repository.PersonalWikiStore
	afterLinked func()
	lookups     int
}

func (w *wikiLinkOutputObserver) VerifiedLinkedPages(
	ctx context.Context, tenant uint64, kb string, authorized, slugs []string,
) ([]*types.WikiPage, bool, error) {
	w.lookups++
	pages, partial, err := w.PersonalWikiStore.VerifiedLinkedPages(ctx, tenant, kb, authorized, slugs)
	if w.afterLinked != nil {
		w.afterLinked()
	}
	return pages, partial, err
}

func TestPortalWikiLinkedLabelsRequireLiveSameScopeTargets(t *testing.T) {
	for name, mutation := range map[string]string{
		"visible":           "",
		"target_archived":   "UPDATE wiki_pages SET status='archived' WHERE id='linked-target'",
		"target_edited":     "UPDATE wiki_pages SET content='changed' WHERE id='linked-target'",
		"source_changed":    "UPDATE chunks SET content='changed' WHERE knowledge_id='public-two-doc'",
		"key_revoked":       "UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1",
		"space_unpublished": "UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=1",
	} {
		t.Run(name, func(t *testing.T) {
			f, store, _, _, _ := wikiReadSessionFixture(t)
			repo := repository.NewWikiPageRepository(f.DB)
			gen, err := repository.BeginWikiMCPGeneration(context.Background(), repo, "public-two")
			require.NoError(t, err)
			for _, page := range []*types.WikiPage{
				{
					ID: "linked-target", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/dpd",
					Title: "链路探测 DPD", Content: "DPD detects peer liveness.", Version: 1,
					Status: types.WikiPageStatusPublished, PageType: types.WikiPageTypeConcept,
					SourceRefs: types.StringArray{"public-two-doc|source"},
				},
				{
					ID: "link-source", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/link-source",
					Title: "source", Content: "启用 [[concept/dpd|DPD]]；再次 [[concept/dpd]]。" +
						"[[concept/foreign|跨空间秘密]] [[concept/unsealed|未验证秘密]] [[missing|不存在名称]]",
					Version: 1, Status: types.WikiPageStatusPublished, PageType: types.WikiPageTypeConcept,
					SourceRefs: types.StringArray{"public-two-doc|source"},
				},
			} {
				require.NoError(t, repo.Create(gen, page))
			}
			for _, page := range []*types.WikiPage{
				{
					ID: "foreign-target", TenantID: 1, KnowledgeBaseID: "public-kb", Slug: "concept/foreign",
					Title: "跨空间秘密", Content: "private", Status: types.WikiPageStatusPublished,
				},
				{
					ID: "unsealed-target", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/unsealed",
					Title: "未验证秘密", Content: "old", Status: types.WikiPageStatusPublished,
				},
			} {
				require.NoError(t, f.DB.Create(page).Error)
			}
			observer := &wikiLinkOutputObserver{PersonalWikiStore: store}
			if mutation != "" {
				observer.afterLinked = func() { require.NoError(t, f.DB.Exec(mutation).Error) }
			}
			s := publishedServiceM7(t, f)
			s.Wiki = observer
			result, err := s.PersonalWiki(fixture.Key("A", 1, 1, true), "wiki_read_page", "",
				"public-two", "concept/link-source", 1, 0)
			require.Equal(t, 1, observer.lookups, "all target slugs share one bounded verified lookup")
			if mutation != "" {
				require.Error(t, err, "linked labels cannot outlive revocation or source/page changes")
				require.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.Len(t, result.Pages, 1)
			require.Contains(t, result.Pages[0].Content, "启用 DPD；再次 链路探测 DPD。")
			for _, hidden := range []string{"跨空间秘密", "未验证秘密", "不存在名称", "[[", "]]"} {
				require.NotContains(t, result.Pages[0].Content, hidden)
			}
		})
	}
}
