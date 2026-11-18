package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/models/chat"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPortalTechnicalWikiAndGenerationPreserveLiterals(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	types.EnableLegacyReadGuards(f.DB)
	r := repository.NewWikiPageRepository(f.DB)
	ctx, err := repository.BeginWikiMCPGeneration(context.Background(), r, "public-two")
	require.NoError(t, err)
	body := "NETCONF `<get-config>`\n```xml\n<rpc><get/></rpc>\n```\n&lt;filter&gt; %2F"
	messages, err := sanitizeWikiGenerationMessages(ctx, []chat.Message{{Content: body}})
	require.NoError(t, err)
	require.Equal(t, body, messages[0].Content)
	embedder := &wikiGuardedEmbedder{gate: repository.NewModelRepository(f.DB).(legacyModelInputGate), tenant: 1}
	texts, err := embedder.texts(ctx, []string{body})
	require.NoError(t, err)
	require.Equal(t, []string{body}, texts)
	page := &types.WikiPage{ID: "technical-page", TenantID: 1, KnowledgeBaseID: "public-two", Slug: "concept/netconf", Title: "NETCONF <get-config>", Content: body, Status: types.WikiPageStatusPublished, PageType: types.WikiPageTypeConcept, SourceRefs: types.StringArray{"public-two-doc|source"}, Version: 1}
	require.NoError(t, r.Create(ctx, page))
	s := publishedServiceM7(t, f)
	key := fixture.Key("A", 1, 1, true)
	result, err := s.PersonalWiki(key, "wiki_read_page", "", "public-two", page.Slug, 50, 0)
	require.NoError(t, err)
	require.Len(t, result.Pages, 1)
	require.Equal(t, body, result.Pages[0].Content)
	require.Equal(t, page.Title, result.Pages[0].Title)
	result, err = s.PersonalWiki(key, "wiki_search", "<get-config>", "public-two", "", 50, 0)
	require.NoError(t, err)
	require.Len(t, result.Pages, 1)
}
