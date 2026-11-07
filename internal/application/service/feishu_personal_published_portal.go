package service

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// BindPersonalPublishedSnapshotReads attaches the existing receipt verifier to
// the existing personal MCP coordinator. It enables no listener or Portal grant.
func BindPersonalPublishedSnapshotReads(portal *PortalMCPKnowledge, personal *FeishuPersonalService) {
	if portal != nil && personal != nil {
		portal.personalRecovery = personal.recovery
	}
}

func (s *PortalMCPKnowledge) publishedDocumentContext(ctx context.Context, id string) (context.Context, error) {
	if s.personalRecovery == nil {
		return ctx, nil
	}
	ks, ok := s.personalRecovery.service.knowledge.(*knowledgeService)
	if !ok {
		return ctx, access.ErrForbidden
	}
	read, _, err := ks.exactPublicReadContext(ctx, id)
	return read, err
}

// The Portal target and Key scope remain authoritative. Within that target,
// independently verify each personal publication before the actual driver SQL.
// Never turn an empty ordinary allow-set into an unrestricted search.
func (s *PortalMCPKnowledge) retrievePublishedPortalDocuments(ctx context.Context, target *access.PortalKBTarget,
	engine access.DocumentRetriever, params types.RetrieveParams,
) ([]*types.RetrieveResult, bool, error) {
	if s.personalRecovery == nil {
		groups, err := engine.Retrieve(ctx, params)
		return groups, false, err
	}
	ids := target.DocumentIDs()
	if len(ids) == 0 {
		return nil, false, nil
	}
	var metadata []struct{ ID, Channel string }
	if err := s.personalRecovery.service.db.WithContext(ctx).Table("knowledges").Select("id,channel").
		Where("id IN ? AND tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL",
			ids, target.Descriptor().TenantID, target.Descriptor().ID).
		Scan(&metadata).Error; err != nil {
		return nil, false, err
	}
	if len(metadata) != len(ids) {
		return nil, false, access.ErrForbidden
	}
	var personal, ordinary []string
	for _, row := range metadata {
		if row.Channel == "feishu_personal" {
			personal = append(personal, row.ID)
		} else {
			ordinary = append(ordinary, row.ID)
		}
	}
	if len(personal) == 0 {
		groups, err := engine.Retrieve(ctx, params)
		return groups, false, err
	}
	var groups []*types.RetrieveResult
	if len(ordinary) > 0 {
		narrow := params
		narrow.KnowledgeIDs = ordinary
		rows, err := engine.Retrieve(ctx, narrow)
		if err != nil {
			return nil, false, err
		}
		groups = append(groups, rows...)
	}
	partial := false
	for _, id := range personal {
		read, err := s.publishedDocumentContext(ctx, id)
		if errors.Is(err, errPersonalExactDocument) {
			partial = true
			continue
		}
		if err != nil {
			return nil, partial, err
		}
		narrow := params
		narrow.KnowledgeIDs = []string{id}
		narrow, err = access.GuardPortalRetrieval(read, engine.EngineType(), narrow)
		if err != nil {
			return nil, partial, err
		}
		rows, err := engine.Retrieve(read, narrow)
		if err != nil {
			return nil, partial, err
		}
		if _, err := types.CheckExactPublishedDocumentRead(read); err != nil {
			return nil, partial, err
		}
		groups = append(groups, rows...)
	}
	return groups, partial, nil
}

func (s *PortalMCPKnowledge) validatePublishedPortalHits(ctx context.Context, hits []PortalMCPHit) error {
	seen := make(map[string]bool)
	for _, hit := range hits {
		if seen[hit.DocumentID] {
			continue
		}
		seen[hit.DocumentID] = true
		read, err := s.publishedDocumentContext(ctx, hit.DocumentID)
		if err != nil {
			return err
		}
		if _, err := types.CheckExactPublishedDocumentRead(read); err != nil {
			return err
		}
	}
	return nil
}
