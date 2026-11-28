package service

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// Only sealed, currently visible exact targets in the same KB may supply link
// labels. The returned dependencies must pass the final live output fence too.
func (s *PortalMCPKnowledge) visibleWikiLinks(
	ctx context.Context, target *access.PortalKBTarget, pages []*types.WikiPage, includeBody bool,
) (map[string]*types.WikiPage, []*types.WikiPage, bool, error) {
	visible := make(map[string]*types.WikiPage)
	for _, page := range pages {
		visible[page.Slug] = page
	}
	wanted := make(map[string]bool)
	var slugs []string
	truncated := false
	for _, page := range pages {
		text := page.Title
		if includeBody {
			text += "\n" + page.Content
		}
		for _, link := range markdownWikiLinks(text) {
			if visible[link.slug] != nil || wanted[link.slug] || !personalWikiSlug.MatchString(link.slug) ||
				strings.Contains(link.slug, "..") || len(link.slug) > 255 {
				continue
			}
			wanted[link.slug] = true
			if len(slugs) >= 200 {
				truncated = true
				continue
			}
			slugs = append(slugs, link.slug)
		}
	}
	if len(slugs) == 0 {
		return visible, nil, truncated, nil
	}
	reader, ok := s.Wiki.(interface {
		VerifiedLinkedPages(context.Context, uint64, string, []string, []string) ([]*types.WikiPage, bool, error)
	})
	if !ok {
		// Older/custom adapters never gain authority from raw link labels.
		return visible, nil, true, nil
	}
	d := target.Descriptor()
	linked, partial, err := reader.VerifiedLinkedPages(ctx, d.TenantID, d.ID, target.DocumentIDs(), slugs)
	if err != nil {
		return nil, nil, false, err
	}
	for _, page := range linked {
		if page == nil || page.TenantID != d.TenantID || page.KnowledgeBaseID != d.ID || !wanted[page.Slug] {
			return nil, nil, false, access.ErrForbidden
		}
		visible[page.Slug] = page
	}
	return visible, linked, truncated || partial, nil
}

func renderVisibleWikiLinks(text string, visible map[string]*types.WikiPage) string {
	rendered, _ := rewriteMarkdownWikiLinks(text, func(link markdownWikiLink) string {
		target := visible[link.slug]
		if target == nil {
			return ""
		}
		if link.display != "" {
			return link.display
		}
		// A target title may contain further Wiki syntax. It cannot grant access
		// to those further targets; remove that syntax rather than expanding it.
		label := strings.TrimSpace(personalWikiLink.ReplaceAllString(target.Title, ""))
		if label == "" {
			return target.Slug
		}
		return label
	})
	return rendered
}
