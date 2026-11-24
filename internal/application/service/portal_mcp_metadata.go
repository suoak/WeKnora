package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

type portalKBMetadataLoader interface {
	LoadPortalMCPMetadata(context.Context, []*access.PortalKBTarget) (map[string]access.PortalKBMetadata, error)
}

// KnowledgeBaseMetadata enriches this request's resolved scope. Counts are
// derived from its readable document identities, never the KB's total assets.
func (s *PortalMCPKnowledge) KnowledgeBaseMetadata(
	ctx context.Context, targets []*access.PortalKBTarget,
) (map[string]access.PortalKBMetadata, error) {
	if s == nil || s.Resolver == nil {
		return nil, access.ErrForbidden
	}
	loader, ok := s.Resolver.Directory.(portalKBMetadataLoader)
	if !ok {
		return nil, access.ErrForbidden
	}
	metadata, err := loader.LoadPortalMCPMetadata(ctx, targets)
	if err != nil {
		return nil, err
	}
	readable, _, err := s.readablePortalDocumentIDs(ctx, targets)
	if err != nil {
		return nil, err
	}
	for _, target := range targets {
		d := target.Descriptor()
		row, found := metadata[d.ID]
		if !found {
			return nil, access.ErrForbidden
		}
		row.DocumentCount = len(readable[d.ID])
		row.Description, err = access.SanitizePortalOutputText(row.Description)
		if err != nil {
			return nil, err
		}
		row.CreatorName, err = access.SanitizePortalOutputText(row.CreatorName)
		if err != nil {
			return nil, err
		}
		metadata[d.ID] = row
	}
	return metadata, nil
}

// Reuse the same document-count scope for KB metadata and document pagination.
// The caller retains the live target/output fence around this projection.
func (s *PortalMCPKnowledge) readablePortalDocumentIDs(
	ctx context.Context, targets []*access.PortalKBTarget,
) (map[string][]string, map[string]bool, error) {
	// Only personal publications require independent per-document proofs. Batch
	// classify identities first so ordinary documents incur no per-row queries.
	personal := map[string]bool{}
	if !ordinaryLocalSourceRead(ctx) && s.personalRecovery != nil {
		var ids []string
		for _, target := range targets {
			ids = append(ids, target.DocumentIDs()...)
		}
		for start := 0; start < len(ids); start += 500 {
			var selected []string
			err := s.personalRecovery.service.db.WithContext(ctx).Table("knowledges k").Select("k.id").
				Where("k.id IN ? AND (k.channel='feishu_personal' OR EXISTS "+
					"(SELECT 1 FROM task_pending_ops o WHERE o.scope=? AND o.scope_id=k.id))",
					ids[start:min(start+500, len(ids))], types.PersonalExactReceiptScope).
				Scan(&selected).Error
			if err != nil {
				return nil, nil, err
			}
			for _, id := range selected {
				personal[id] = true
			}
		}
	}
	readable := make(map[string][]string, len(targets))
	var verified []PortalMCPHit
	for _, target := range targets {
		id := target.Descriptor().ID
		readable[id] = []string{}
		for _, document := range target.DocumentIDs() {
			if personal[document] {
				read, err := s.publishedDocumentContext(ctx, document)
				if errors.Is(err, errPersonalExactDocument) {
					continue
				}
				if err != nil {
					return nil, nil, err
				}
				if _, err := types.CheckExactPublishedDocumentRead(read); err != nil {
					return nil, nil, err
				}
				verified = append(verified, PortalMCPHit{DocumentID: document})
			}
			readable[id] = append(readable[id], document)
		}
	}
	if err := s.validatePublishedPortalHits(ctx, verified); err != nil {
		return nil, nil, err
	}
	return readable, personal, nil
}
