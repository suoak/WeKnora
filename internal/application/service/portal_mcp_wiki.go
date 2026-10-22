package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

type PersonalWikiReader interface {
	VerifiedPages(context.Context, uint64, string, []string) ([]*types.WikiPage, bool, error)
	RevalidatePages(context.Context, uint64, string, []string, []*types.WikiPage) error
}
type PortalWikiPage struct {
	TenantID  uint64 `json:"tenant_id"`
	KBID      string `json:"knowledge_base_id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Content   string `json:"content,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}
type PortalWikiResult struct {
	Pages      []PortalWikiPage `json:"pages"`
	Status     string           `json:"status"`
	Warning    string           `json:"warning,omitempty"`
	Truncated  bool             `json:"truncated"`
	NextOffset int              `json:"next_offset,omitempty"`
}

var personalWikiLink = regexp.MustCompile(`\[\[[^\]]*\]\]`)
var personalWikiSlug = regexp.MustCompile(`^[\p{L}\p{N}_-]+(?:/[\p{L}\p{N}_.-]+)*$`)

// PersonalWiki shares the five-tool resolver and live grants. It never calls
// GetIndex/GetIndexView or any create/update/Agent entry point.
func (s *PortalMCPKnowledge) PersonalWiki(ctx context.Context, operation, query, kb, slug string, limit, offset int, requested ...string) (*PortalWikiResult, error) {
	if s == nil || s.Wiki == nil || limit < 1 || limit > 200 || offset < 0 || offset > 1000 || len(query) > 4096 || len(slug) > 255 || len(kb) > 512 {
		return nil, access.ErrForbidden
	}
	if operation != "wiki_search" && operation != "wiki_read_page" && operation != "wiki_index" {
		return nil, access.ErrForbidden
	}
	if operation == "wiki_search" && strings.TrimSpace(query) == "" || operation == "wiki_read_page" && slug == "" || operation == "wiki_index" && kb == "" {
		return nil, access.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ctx, diagnostics := access.WithPortalDiagnostics(ctx)
	if len(requested) > 128 || len(requested) > 0 && operation != "wiki_search" || operation == "wiki_search" && limit > 50 {
		return nil, access.ErrForbidden
	}
	targets, e := s.Resolve(ctx, requested)
	if e != nil {
		return nil, e
	}
	result := &PortalWikiResult{Pages: []PortalWikiPage{}, Status: "complete"}
	var used []*access.PortalKBTarget
	type readSet struct {
		target *access.PortalKBTarget
		pages  []*types.WikiPage
	}
	var readSets []readSet
	matchedKB := false
	for _, target := range targets {
		d := target.Descriptor()
		if kb != "" && d.ID != kb && d.Name != kb {
			continue
		}
		matchedKB = true
		if len(used) >= 128 {
			return nil, access.ErrForbidden
		}
		if e = target.Revalidate(ctx); e != nil {
			return nil, e
		}
		pages, partial, err := s.Wiki.VerifiedPages(ctx, d.TenantID, d.ID, target.DocumentIDs())
		if err != nil {
			if errors.Is(err, types.ErrLegacySourceRestricted) || errors.Is(err, repository.ErrWikiMCPUnverified) {
				result.Status = "partial"
				continue
			}
			return nil, err
		}
		if partial {
			result.Status = "partial"
			result.Truncated = true
		}
		for _, page := range pages {
			if !personalWikiSlug.MatchString(page.Slug) || strings.Contains(page.Slug, "..") {
				result.Status = "partial"
				continue
			}
			if operation == "wiki_read_page" && page.Slug != slug {
				continue
			}
			if operation == "wiki_search" && !strings.Contains(strings.ToLower(page.Title+"\n"+page.Summary+"\n"+page.Content), strings.ToLower(query)) {
				continue
			}
			title, err := access.SanitizePortalText(personalWikiLink.ReplaceAllString(page.Title, ""))
			if err != nil {
				return nil, err
			}
			body, err := access.SanitizePortalText(personalWikiLink.ReplaceAllString(page.Content, ""))
			if err != nil {
				return nil, err
			}
			row := PortalWikiPage{TenantID: d.TenantID, KBID: d.ID, Slug: page.Slug, Title: title}
			if operation == "wiki_read_page" {
				text := []rune(body)
				if len(text) > 65536 {
					text = text[:65536]
					row.Truncated = true
				}
				row.Content = string(text)
			}
			result.Pages = append(result.Pages, row)
		}
		used = append(used, target)
		readSets = append(readSets, readSet{target: target, pages: pages})
		if e = target.Revalidate(ctx); e != nil {
			return nil, e
		}
	}
	if kb != "" && !matchedKB {
		return nil, access.ErrForbidden
	}
	if operation == "wiki_read_page" {
		if len(result.Pages) == 0 {
			return nil, access.ErrForbidden
		}
		if len(result.Pages) > 1 {
			result.Status = "ambiguous"
			result.Warning = "Slug exists in multiple authorized knowledge bases; specify knowledge_base_id."
			for i := range result.Pages {
				result.Pages[i].Content = ""
			}
		}
	}
	if kb != "" && operation != "wiki_search" && len(used) > 1 {
		result.Status = "ambiguous"
		result.Warning = "Knowledge base name matches multiple authorized KBs; specify the exact knowledge_base_id."
		for i := range result.Pages {
			result.Pages[i].Content = ""
		}
	}
	if len(result.Pages) > offset+limit {
		result.Truncated = true
		result.NextOffset = offset + limit
	}
	if offset > len(result.Pages) {
		offset = len(result.Pages)
	}
	end := offset + limit
	if end > len(result.Pages) {
		end = len(result.Pages)
	}
	result.Pages = result.Pages[offset:end]
	if diagnostics.Partial() {
		result.Status = "partial"
	}
	if result.Status == "partial" {
		result.Warning = "Some sources or generated pages are unavailable pending verified regeneration; this is not the complete knowledge scope."
	}
	if e = validatePortalTargets(ctx, used); e != nil {
		return nil, e
	}
	for _, set := range readSets {
		d := set.target.Descriptor()
		if e = s.Wiki.RevalidatePages(ctx, d.TenantID, d.ID, set.target.DocumentIDs(), set.pages); e != nil {
			return nil, e
		}
	}
	return result, nil
}
