package chatpipeline

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"testing"
)

type heldLegacyGraphKnowledge struct{ interfaces.KnowledgeRepository }

func (*heldLegacyGraphKnowledge) CheckLegacyKBRead(context.Context, string) error {
	return types.ErrLegacySourceRestricted
}

type forbiddenLegacyGraph struct {
	interfaces.RetrieveGraphRepository
	t *testing.T
}

func (g *forbiddenLegacyGraph) SearchNode(context.Context, types.NameSpace, []string) (*types.GraphData, error) {
	g.t.Error("held source reached graph backend")
	return &types.GraphData{}, nil
}
func TestFeishuLegacyHoldStopsGraphCandidateReads(t *testing.T) {
	for _, document := range []bool{false, true} {
		state := types.PipelineState{Entity: []string{"sensitivecanary"}, EntityKBIDs: []string{"held-kb"}}
		if document {
			state.EntityKBIDs = nil
			state.EntityKnowledge = map[string]string{"document": "held-kb"}
		}
		cm := &types.ChatManage{PipelineState: state}
		p := &PluginSearchEntity{knowledgeRepo: &heldLegacyGraphKnowledge{}, graphRepo: &forbiddenLegacyGraph{t: t}}
		require.Nil(t, p.OnEvent(context.Background(), types.ENTITY_SEARCH, cm, func() *PluginError { return nil }))
		require.Empty(t, cm.GraphResult.Node)
		require.Empty(t, cm.SearchResult)
	}
}
