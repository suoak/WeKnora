package chatpipeline

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/spreadsheetquery"
	"github.com/Tencent/WeKnora/internal/types"
)

// PluginSpreadsheetQuery replaces sampled row context with deterministic
// parser-index output only for completeness-sensitive spreadsheet queries.
type PluginSpreadsheetQuery struct {
	service *spreadsheetquery.Service
}

func NewPluginSpreadsheetQuery(
	eventManager *EventManager,
	service *spreadsheetquery.Service,
) *PluginSpreadsheetQuery {
	plugin := &PluginSpreadsheetQuery{service: service}
	eventManager.Register(plugin)
	return plugin
}

func (p *PluginSpreadsheetQuery) ActivationEvents() []types.EventType {
	return []types.EventType{types.SPREADSHEET_QUERY}
}

func (p *PluginSpreadsheetQuery) OnEvent(
	ctx context.Context,
	eventType types.EventType,
	chatManage *types.ChatManage,
	next func() *PluginError,
) *PluginError {
	switch chatManage.RetrievalQueryType {
	case types.RetrievalQueryExhaustiveList, types.RetrievalQueryCount, types.RetrievalQueryFilter:
	default:
		return next()
	}

	result, err := p.service.Execute(ctx, spreadsheetquery.Request{
		TenantID:      chatManage.TenantID,
		Query:         chatManage.Query,
		QueryType:     chatManage.RetrievalQueryType,
		SearchTargets: chatManage.SearchTargets,
		Candidates:    chatManage.MergeResult,
	})
	if err != nil || !result.Found || result.Source == nil {
		chatManage.RetrievalCompleteness = types.PartialRetrievalCompleteness()
		return next()
	}

	synthetic := *result.Source
	synthetic.Content = result.Content
	synthetic.MatchedContent = ""
	synthetic.ChunkType = string(types.ChunkTypeSheetEntityIndex)
	chatManage.MergeResult = []*types.SearchResult{&synthetic}
	chatManage.RetrievalCompleteness = result.Completeness
	return next()
}
