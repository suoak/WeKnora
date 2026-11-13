package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type unavailableAgentCatalog struct{ interfaces.KnowledgeService }

func (unavailableAgentCatalog) ListPagedKnowledgeByKnowledgeBaseID(
	context.Context, string, *types.Pagination, types.KnowledgeListFilter,
) (*types.PageResult, error) {
	return nil, errors.New("catalog denied")
}

func TestAgentCatalogFailureKeepsCountUnknown(t *testing.T) {
	s := &agentService{
		knowledgeBaseService: &fakeAgentKnowledgeBaseService{kb: &types.KnowledgeBase{
			ID: "kb", Name: "ELOG", TenantID: 1, Type: types.KnowledgeBaseTypeDocument,
		}},
		knowledgeService: unavailableAgentCatalog{},
	}
	infos, err := s.getKnowledgeBaseInfos(t.Context(), []string{"kb"}, nil)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	require.Equal(t, -1, infos[0].DocCount)
	require.Empty(t, infos[0].RecentDocs)
}
