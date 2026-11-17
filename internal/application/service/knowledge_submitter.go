package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// Optional HTTP presentation capability, separate from source/model read APIs.
func (s *knowledgeService) ReadKnowledgeSubmitters(
	ctx context.Context, tenant uint64, kb string, ids []string,
) (map[string]*types.KnowledgeSubmitter, error) {
	reader, ok := s.repo.(interface {
		ReadKnowledgeSubmitters(context.Context, uint64, string, []string) (map[string]*types.KnowledgeSubmitter, error)
	})
	if !ok {
		return nil, nil
	}
	return reader.ReadKnowledgeSubmitters(ctx, tenant, kb, ids)
}
