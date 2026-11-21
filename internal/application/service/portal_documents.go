package service

import (
	"context"
	"errors"
	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"sort"
	"strings"
	"time"
)

type portalDocumentMetadataSearch interface {
	SearchPortalDocumentMetadata(context.Context, uint64, string, []string, string, int) ([]*types.Knowledge, error)
}
type PortalMCPDocument struct {
	PortalMCPHit
	FileName    string    `json:"file_name"`
	ParseStatus string    `json:"parse_status"`
	UpdatedAt   time.Time `json:"updated_at"`
	MatchRank   int       `json:"match_rank"`
}
type PortalMCPDocumentSearchResult struct {
	Status    string              `json:"status"`
	Documents []PortalMCPDocument `json:"documents"`
	Limit     int                 `json:"limit"`
	Truncated bool                `json:"truncated"`
}

func documentMatchRank(query, title, file string) int {
	query = strings.ToLower(query)
	title = strings.ToLower(title)
	file = strings.ToLower(file)
	if title == query || file == query {
		return 0
	}
	if strings.HasPrefix(title, query) || strings.HasPrefix(file, query) {
		return 1
	}
	return 2
}

func (s *PortalMCPKnowledge) SearchDocuments(ctx context.Context, ids []string, query string, limit int) (*PortalMCPDocumentSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 4096 || limit < 1 || limit > 100 {
		return nil, access.ErrForbidden
	}
	if err := access.ValidatePortalQuery(query); err != nil {
		return nil, err
	}
	targets, err := s.Resolve(ctx, ids)
	if err != nil {
		return nil, err
	}
	repo, ok := s.Knowledge.(portalDocumentMetadataSearch)
	if !ok {
		return nil, access.ErrForbidden
	}
	out := &PortalMCPDocumentSearchResult{Status: "complete", Documents: []PortalMCPDocument{}, Limit: limit}
	if len(targets) == 0 {
		out.Status = "no_scope"
	}
	for _, target := range targets {
		execution, e := target.SearchContext(ctx)
		if e != nil {
			return nil, e
		}
		d := target.Descriptor()
		rows, e := repo.SearchPortalDocumentMetadata(execution, d.TenantID, d.ID, target.DocumentIDs(), query, limit+1)
		if e != nil {
			return nil, e
		}
		for _, row := range rows {
			canonical, e := s.document(ctx, target, row.ID)
			if errors.Is(e, errPersonalExactDocument) {
				out.Status = "partial"
				continue
			}
			if e != nil {
				return nil, e
			}
			file, e := access.SanitizePortalText(canonical.FileName)
			if e != nil {
				return nil, e
			}
			if !strings.Contains(strings.ToLower(canonical.Title), strings.ToLower(query)) && !strings.Contains(strings.ToLower(file), strings.ToLower(query)) {
				return nil, access.ErrForbidden
			}
			out.Documents = append(out.Documents, PortalMCPDocument{PortalMCPHit: portalHit(target, canonical, nil), FileName: file, ParseStatus: canonical.ParseStatus, UpdatedAt: canonical.UpdatedAt, MatchRank: documentMatchRank(query, canonical.Title, file)})
		}
	}
	sort.Slice(out.Documents, func(i, j int) bool {
		a, b := out.Documents[i], out.Documents[j]
		if a.MatchRank != b.MatchRank {
			return a.MatchRank < b.MatchRank
		}
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		return a.Reference < b.Reference
	})
	if len(out.Documents) > limit {
		out.Truncated = true
		out.Documents = out.Documents[:limit]
	}
	hits := make([]PortalMCPHit, 0, len(out.Documents))
	for _, row := range out.Documents {
		hits = append(hits, row.PortalMCPHit)
	}
	if err = validatePortalTargets(ctx, targets); err != nil {
		return nil, err
	}
	if err = s.validatePublishedPortalHits(ctx, hits); err != nil {
		return nil, err
	}
	return out, nil
}
