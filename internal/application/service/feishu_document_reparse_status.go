package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

func (s *knowledgeService) decorateSourceDocumentReparse(ctx context.Context, rows []*types.Knowledge) {
	groups := map[uint64][]string{}
	byID := map[string]*types.Knowledge{}
	for _, row := range rows {
		if row != nil && legacyWikiDrive(row.Channel) {
			groups[row.TenantID] = append(groups[row.TenantID], row.ID)
			byID[row.ID] = row
		}
	}
	if len(groups) == 0 {
		return
	}
	store, err := repository.FeishuLegacyStoreFor(s.repo)
	if err != nil {
		return // Optional metadata projection must not interrupt authorized reads.
	}
	for tenant, ids := range groups {
		statuses, err := store.SourceDocumentReparseStatuses(ctx, tenant, ids)
		if err != nil {
			logger.Warnf(ctx, "Document reparse catalog progress is temporarily unavailable")
			continue
		}
		for i := range statuses {
			if row := byID[statuses[i].KnowledgeID]; row != nil && row.TenantID == tenant {
				row.SourceDocumentReparse = &statuses[i]
			}
		}
	}
}
