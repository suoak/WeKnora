package handler

import (
	"context"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Called only after HTTP document/KB access checks. No attribution is persisted
// onto objects used by background parsing, model input or publication proofs.
func (h *KnowledgeHandler) decorateKnowledgeSubmitters(
	ctx context.Context, tenant uint64, kb string, rows []*types.Knowledge,
) {
	reader, ok := h.kgService.(interface {
		ReadKnowledgeSubmitters(context.Context, uint64, string, []string) (map[string]*types.KnowledgeSubmitter, error)
	})
	if !ok {
		return
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row != nil && row.TenantID == tenant && row.KnowledgeBaseID == kb {
			ids = append(ids, row.ID)
		}
	}
	values, err := reader.ReadKnowledgeSubmitters(ctx, tenant, kb, ids)
	if err != nil {
		logger.Warnf(ctx, "Document submitter projection unavailable")
		return
	}
	for _, row := range rows {
		if row != nil && row.TenantID == tenant && row.KnowledgeBaseID == kb {
			row.Submitter = values[row.ID]
		}
	}
}
