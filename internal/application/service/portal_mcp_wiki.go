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
	"golang.org/x/sync/errgroup"
)

type PersonalWikiReader interface {
	VerifiedPages(context.Context, uint64, string, []string) ([]*types.WikiPage, bool, error)
	RevalidatePages(context.Context, uint64, string, []string, []*types.WikiPage) error
}

const portalWikiCandidateConcurrency = 2

type portalWikiCandidate struct {
	pages   []*types.WikiPage
	partial bool
	err     error
}

func (s *PortalMCPKnowledge) wikiCandidatePages(
	ctx context.Context, target *access.PortalKBTarget, operation, query, slug string, limit, offset int,
) ([]*types.WikiPage, bool, error) {
	d := target.Descriptor()
	if search, ok := s.Wiki.(interface {
		VerifiedSearchPages(context.Context, uint64, string, []string, string) ([]*types.WikiPage, bool, error)
	}); ok && operation == "wiki_search" {
		return search.VerifiedSearchPages(ctx, d.TenantID, d.ID, target.DocumentIDs(), query)
	}
	if bounded, ok := s.Wiki.(interface {
		VerifiedPagesWindow(context.Context, uint64, string, []string, string, int) ([]*types.WikiPage, bool, error)
	}); ok && operation != "wiki_search" {
		take, exactSlug := min(200, offset+limit+1), ""
		if operation == "wiki_read_page" {
			exactSlug, take = slug, 1
		}
		return bounded.VerifiedPagesWindow(ctx, d.TenantID, d.ID, target.DocumentIDs(), exactSlug, take)
	}
	return s.Wiki.VerifiedPages(ctx, d.TenantID, d.ID, target.DocumentIDs())
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

var (
	personalWikiLink = regexp.MustCompile(`\[\[[^\]]*\]\]`)
	personalWikiSlug = regexp.MustCompile(`^[\p{L}\p{N}_-]+(?:/[\p{L}\p{N}_.-]+)*$`)
)

// PersonalWiki shares the five-tool resolver and live grants. It never calls
// GetIndex/GetIndexView or any create/update/Agent entry point.
func (s *PortalMCPKnowledge) PersonalWiki(
	ctx context.Context,
	operation, query, kb, slug string,
	limit, offset int,
	requested ...string,
) (*PortalWikiResult, error) {
	if s == nil || s.Wiki == nil || limit < 1 || limit > 200 || offset < 0 || offset > 1000 || len(query) > 4096 ||
		len(slug) > 255 ||
		len(kb) > 512 {
		return nil, access.ErrForbidden
	}
	if operation != "wiki_search" && operation != "wiki_read_page" && operation != "wiki_index" {
		return nil, access.ErrForbidden
	}
	if operation == "wiki_search" && strings.TrimSpace(query) == "" || operation == "wiki_read_page" && slug == "" ||
		operation == "wiki_index" && kb == "" {
		return nil, access.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ctx = repository.WithWikiReadProofSession(ctx)
	ctx = access.WithPortalSnapshotReads(access.WithoutPortalSnapshotReads(ctx))
	ctx, diagnostics := access.WithPortalDiagnostics(ctx)
	if len(requested) > 128 || len(requested) > 0 && operation != "wiki_search" ||
		operation == "wiki_search" && limit > 50 {
		return nil, access.ErrForbidden
	}
	// A single selected Wiki must not authorize every unrelated knowledge base
	// before filtering by its name/ID. The resolver still checks live authority.
	if kb != "" && len(requested) == 0 {
		requested = []string{kb}
	}
	targets, e := s.Resolve(ctx, requested)
	if e != nil {
		return nil, e
	}
	// Each target keeps its complete source proof. Bound parallel reads rather
	// than spending the entire unchanged deadline on an early serial prefix.
	candidates := make([]portalWikiCandidate, len(targets))
	group, candidateCtx := errgroup.WithContext(ctx)
	group.SetLimit(portalWikiCandidateConcurrency)
	for i, target := range targets {
		d := target.Descriptor()
		if kb != "" && d.ID != kb && d.Name != kb {
			continue
		}
		group.Go(func() error {
			if err := target.Revalidate(candidateCtx); err != nil {
				return err
			}
			types.ObserveMCPKnowledgeBase(candidateCtx, d.ID)
			out := &candidates[i]
			out.pages, out.partial, out.err = s.wikiCandidatePages(
				candidateCtx, target, operation, query, slug, limit, offset,
			)
			if out.err != nil && !errors.Is(out.err, types.ErrLegacySourceRestricted) &&
				!errors.Is(out.err, repository.ErrWikiMCPUnverified) {
				return out.err
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	result := &PortalWikiResult{Pages: []PortalWikiPage{}, Status: "complete"}
	var used []*access.PortalKBTarget
	type readSet struct {
		target *access.PortalKBTarget
		pages  []*types.WikiPage
	}
	var readSets []readSet
	matchedKB := false
	for i, target := range targets {
		d := target.Descriptor()
		if kb != "" && d.ID != kb && d.Name != kb {
			continue
		}
		matchedKB = true
		if len(used) >= 128 {
			return nil, access.ErrForbidden
		}
		pages, partial, err := candidates[i].pages, candidates[i].partial, candidates[i].err
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
			if operation == "wiki_search" &&
				!strings.Contains(
					strings.ToLower(page.Title+"\n"+page.Summary+"\n"+page.Content),
					strings.ToLower(query),
				) {
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
		// No page body or title was read when verification returned an empty
		// set. Live target/Key checks below remain mandatory for every target.
		if len(pages) > 0 {
			readSets = append(readSets, readSet{target: target, pages: pages})
		}
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
		result.Warning = "Some sources or generated pages are unavailable pending verified regeneration; " +
			"this is not the complete knowledge scope."
	}
	// Output authorization is a fresh phase; candidate snapshots never
	// authorize a returned title or body after a concurrent policy change.
	ctx = access.WithoutPortalSnapshotReads(ctx)
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
