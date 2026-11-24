package service

import (
	"context"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

type portalDocumentMetadataLoader interface {
	LoadPortalDocumentMetadata(context.Context, uint64, string, []string, bool) ([]*types.Knowledge, error)
}

// PortalMCPListedDocument extends the existing hit contract with safe document metadata.
type PortalMCPListedDocument struct {
	PortalMCPHit
	FileName    string    `json:"file_name"`
	FileType    string    `json:"file_type"`
	Type        string    `json:"type"`
	ParseStatus string    `json:"parse_status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PortalMCPDocumentPage reports the readable total independently of the requested page.
type PortalMCPDocumentPage struct {
	Total     int                       `json:"total"`
	Documents []PortalMCPListedDocument `json:"documents"`
}

// ListDocumentsPage slices authorized identities before selecting document
// metadata. An unfiltered page loads at most page_size rows in one SQL query;
// it never materializes the KB's document bodies. Keyword totals retain the
// original case-insensitive matching on sanitized titles, using title-only
// batches rather than loading every document and filtering after pagination.
func (s *PortalMCPKnowledge) ListDocumentsPage(
	ctx context.Context, targets []*access.PortalKBTarget, keyword string, page, size int,
) (*PortalMCPDocumentPage, error) {
	if len(targets) != 1 || page < 1 || page > 10000 || size < 1 || size > 100 {
		return nil, access.ErrForbidden
	}
	loader, ok := s.Knowledge.(portalDocumentMetadataLoader)
	if !ok {
		return nil, access.ErrForbidden
	}
	readable, personal, err := s.readablePortalDocumentIDs(ctx, targets)
	if err != nil {
		return nil, err
	}
	target := targets[0]
	d := target.Descriptor()
	execution, err := target.SearchContext(ctx)
	if err != nil {
		return nil, err
	}
	ids := readable[d.ID]
	if keyword != "" {
		keyword = strings.ToLower(keyword)
		matched := []string{}
		for start := 0; start < len(ids); start += 500 {
			batch := ids[start:min(start+500, len(ids))]
			rows, err := loader.LoadPortalDocumentMetadata(execution, d.TenantID, d.ID, batch, true)
			if err != nil {
				return nil, err
			}
			byID := make(map[string]*types.Knowledge, len(rows))
			for _, row := range rows {
				byID[row.ID] = row
			}
			for _, id := range batch {
				row := byID[id]
				if row == nil || row.TenantID != d.TenantID || row.KnowledgeBaseID != d.ID {
					return nil, access.ErrForbidden
				}
				title, err := access.SanitizePortalText(row.Title)
				if err != nil {
					return nil, err
				}
				if strings.Contains(strings.ToLower(title), keyword) {
					matched = append(matched, id)
				}
			}
		}
		ids = matched
	}
	out := &PortalMCPDocumentPage{Total: len(ids), Documents: []PortalMCPListedDocument{}}
	start := min((page-1)*size, len(ids))
	selected := ids[start : start+min(size, len(ids)-start)]
	ordinary := []string{}
	for _, id := range selected {
		if !personal[id] {
			ordinary = append(ordinary, id)
		}
	}
	rows, err := loader.LoadPortalDocumentMetadata(execution, d.TenantID, d.ID, ordinary, false)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*types.Knowledge, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	var hits []PortalMCPHit
	for _, id := range selected {
		row := byID[id]
		if personal[id] {
			row, err = s.document(ctx, target, id)
			if err != nil {
				return nil, err
			}
		}
		if row == nil || row.ID != id || row.TenantID != d.TenantID || row.KnowledgeBaseID != d.ID ||
			row.DeletedAt.Valid {
			return nil, access.ErrForbidden
		}
		title, err := access.SanitizePortalText(row.Title)
		if err != nil {
			return nil, err
		}
		file, err := access.SanitizePortalOutputText(row.FileName)
		if err != nil {
			return nil, err
		}
		fileType, err := access.SanitizePortalOutputText(row.FileType)
		if err != nil {
			return nil, err
		}
		kind, err := access.SanitizePortalOutputText(row.Type)
		if err != nil {
			return nil, err
		}
		status, err := access.SanitizePortalOutputText(row.ParseStatus)
		if err != nil {
			return nil, err
		}
		projected := *row
		projected.Title = title
		hit := portalHit(target, &projected, nil)
		hits = append(hits, hit)
		out.Documents = append(out.Documents, PortalMCPListedDocument{
			PortalMCPHit: hit,
			FileName:     file,
			FileType:     fileType,
			Type:         kind,
			ParseStatus:  status,
			CreatedAt:    row.CreatedAt,
			UpdatedAt:    row.UpdatedAt,
		})
	}
	if err := s.validatePublishedPortalHits(ctx, hits); err != nil {
		return nil, err
	}
	return out, nil
}
