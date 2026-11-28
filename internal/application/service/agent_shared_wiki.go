package service

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

type agentSharedWikiStore interface {
	SharedWikiDocumentIDs(context.Context, uint64, string) ([]string, error)
	VerifiedPatternSearchPages(context.Context, uint64, string, []string, string) ([]*types.WikiPage, bool, error)
	VerifiedPagesWindow(context.Context, uint64, string, []string, string, int) ([]*types.WikiPage, bool, error)
	VerifiedLinkedPages(context.Context, uint64, string, []string, []string) ([]*types.WikiPage, bool, error)
	RevalidatePages(context.Context, uint64, string, []string, []*types.WikiPage) error
}

// Read tools must not inherit the browser's tenant-local presentation overlay.
// Foreign shared KBs instead use current published pages with sealed provenance.
// This adapter is never installed on Wiki mutation or generation tools.
type agentSharedWikiReader struct {
	interfaces.WikiPageService
	service *wikiPageService
	store   agentSharedWikiStore
}

func newAgentWikiReader(base interfaces.WikiPageService, db *gorm.DB) interfaces.WikiPageService {
	s, ok := base.(*wikiPageService)
	if !ok || db == nil {
		return base
	}
	return &agentSharedWikiReader{WikiPageService: base, service: s, store: &repository.PersonalWikiStore{DB: db}}
}

func (s *agentSharedWikiReader) sharedKB(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	tenant := types.WorkspaceWebReadTenant(ctx)
	if tenant == 0 {
		return nil, nil
	}
	kb, err := s.service.kbService.GetKnowledgeBaseByIDOnly(ctx, id)
	if err != nil {
		return nil, err
	}
	if kb == nil || kb.ID != id || kb.TenantID == 0 {
		return nil, access.ErrForbidden
	}
	if kb.TenantID == tenant {
		return nil, nil
	}
	if err := access.AuthorizeKBAccess(ctx, s.service.kbShareService, []*types.KnowledgeBase{kb}); err != nil {
		return nil, err
	}
	if !kb.IsWikiEnabled() {
		return nil, access.ErrForbidden
	}
	return kb, nil
}

func (s *agentSharedWikiReader) SearchPages(
	ctx context.Context, kb, query string, limit int,
) ([]*types.WikiPage, error) {
	pages, partial, err := s.SearchPagesWithStatus(ctx, kb, query, limit)
	if err == nil && partial {
		// Callers without a partial-result channel must not interpret a bounded
		// or excluded set as an exhaustive search.
		return nil, repository.ErrWikiMCPUnverified
	}
	return pages, err
}

func (s *agentSharedWikiReader) SearchPagesWithStatus(
	ctx context.Context, id, pattern string, limit int,
) ([]*types.WikiPage, bool, error) {
	kb, err := s.sharedKB(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if kb == nil {
		pages, err := s.WikiPageService.SearchPages(ctx, id, pattern, limit)
		return pages, false, err
	}
	ctx = repository.WithWikiReadProofSession(ctx)
	documents, err := s.store.SharedWikiDocumentIDs(ctx, kb.TenantID, id)
	if err != nil {
		return nil, false, err
	}
	pages, partial, err := s.store.VerifiedPatternSearchPages(ctx, kb.TenantID, id, documents, pattern)
	if err != nil {
		return nil, false, err
	}
	if limit <= 0 {
		limit = 10
	}
	limit = min(limit, 50)
	if len(pages) > limit {
		pages = pages[:limit]
	}
	clean, linksPartial, err := s.checkedPages(ctx, kb, documents, pages)
	return clean, partial || linksPartial, err
}

func (s *agentSharedWikiReader) GetPageBySlug(
	ctx context.Context, id, slug string,
) (*types.WikiPage, error) {
	kb, err := s.sharedKB(ctx, id)
	if err != nil {
		return nil, err
	}
	if kb == nil {
		return s.WikiPageService.GetPageBySlug(ctx, id, slug)
	}
	ctx = repository.WithWikiReadProofSession(ctx)
	documents, err := s.store.SharedWikiDocumentIDs(ctx, kb.TenantID, id)
	if err != nil {
		return nil, err
	}
	pages, partial, err := s.store.VerifiedPagesWindow(ctx, kb.TenantID, id, documents, slug, 1)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		if partial {
			return nil, repository.ErrWikiMCPUnverified
		}
		return nil, repository.ErrWikiPageNotFound
	}
	if len(pages) != 1 || pages[0].Slug != slug {
		return nil, access.ErrForbidden
	}
	clean, _, err := s.checkedPages(ctx, kb, documents, pages)
	if err != nil {
		return nil, err
	}
	return clean[0], nil
}

func (s *agentSharedWikiReader) checkedPages(
	ctx context.Context, kb *types.KnowledgeBase, documents []string, pages []*types.WikiPage,
) ([]*types.WikiPage, bool, error) {
	visible := map[string]*types.WikiPage{}
	wanted := map[string]bool{}
	var slugs []string
	partial := false
	for _, page := range pages {
		if page == nil || page.TenantID != kb.TenantID || page.KnowledgeBaseID != kb.ID ||
			page.Status != types.WikiPageStatusPublished {
			return nil, false, access.ErrForbidden
		}
		visible[page.Slug] = page
	}
	for _, page := range pages {
		text := page.Title + "\n" + page.Summary + "\n" + page.Content + "\n" + strings.Join(page.Aliases, "\n")
		for _, link := range markdownWikiLinks(text) {
			if visible[link.slug] != nil || wanted[link.slug] {
				continue
			}
			wanted[link.slug] = true
			if len(slugs) == 200 || !personalWikiSlug.MatchString(link.slug) ||
				strings.Contains(link.slug, "..") || len(link.slug) > 255 {
				partial = true
				continue
			}
			slugs = append(slugs, link.slug)
		}
	}
	var linked []*types.WikiPage
	if len(slugs) > 0 {
		var err error
		var excluded bool
		linked, excluded, err = s.store.VerifiedLinkedPages(ctx, kb.TenantID, kb.ID, documents, slugs)
		if err != nil {
			return nil, false, err
		}
		partial = partial || excluded
		for _, page := range linked {
			if page == nil || page.TenantID != kb.TenantID || page.KnowledgeBaseID != kb.ID ||
				page.Status != types.WikiPageStatusPublished || !wanted[page.Slug] {
				return nil, false, access.ErrForbidden
			}
			visible[page.Slug] = page
		}
	}
	// Recheck both grants and exact page/source versions after candidate and
	// linked-page proof. Keep original objects unchanged until proof completes.
	if err := access.AuthorizeKBAccess(ctx, s.service.kbShareService, []*types.KnowledgeBase{kb}); err != nil {
		return nil, false, err
	}
	for _, set := range [][]*types.WikiPage{pages, linked} {
		if len(set) > 0 {
			if err := s.store.RevalidatePages(ctx, kb.TenantID, kb.ID, documents, set); err != nil {
				return nil, false, err
			}
		}
	}
	types.RecordLegacyCandidateRead(ctx, kb.ID)
	clean := make([]*types.WikiPage, 0, len(pages))
	for _, page := range pages {
		clone := *page
		render := func(text string) string {
			out, _ := rewriteMarkdownWikiLinks(text, func(link markdownWikiLink) string {
				if visible[link.slug] == nil {
					return ""
				}
				return text[link.start:link.end]
			})
			return out
		}
		clone.Title, clone.Summary, clone.Content = render(page.Title), render(page.Summary), render(page.Content)
		clone.Aliases = nil
		for _, alias := range page.Aliases {
			clone.Aliases = append(clone.Aliases, render(alias))
		}
		// Neighbour metadata is also an output channel for target names.
		clone.InLinks, clone.OutLinks = nil, nil
		stripWikiPageInlineChunkCitations(&clone)
		clean = append(clean, &clone)
	}
	return clean, partial, nil
}
