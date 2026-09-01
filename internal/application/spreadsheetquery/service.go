// Package spreadsheetquery provides the shared repository-backed execution
// path used by chat pipelines and future agent tools.
package spreadsheetquery

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Tencent/WeKnora/internal/spreadsheet"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type Service struct {
	chunks interfaces.ChunkRepository
}

func NewService(chunks interfaces.ChunkRepository) *Service {
	return &Service{chunks: chunks}
}

type Request struct {
	TenantID      uint64
	Query         string
	QueryType     types.RetrievalQueryType
	SearchTargets types.SearchTargets
	Candidates    []*types.SearchResult
}

type Result struct {
	Found        bool
	Content      string
	Completeness types.RetrievalCompleteness
	Source       *types.SearchResult
}

// Execute resolves the first retrieved spreadsheet document with a usable
// parser index. It intentionally does not broaden access beyond candidates
// already selected by the ordinary retrieval pipeline.
func (s *Service) Execute(ctx context.Context, req Request) (Result, error) {
	seen := make(map[string]struct{})
	for _, candidate := range req.Candidates {
		if candidate == nil || candidate.KnowledgeID == "" {
			continue
		}
		if _, ok := seen[candidate.KnowledgeID]; ok {
			continue
		}
		seen[candidate.KnowledgeID] = struct{}{}

		tenantID := req.SearchTargets.GetTenantIDForKB(candidate.KnowledgeBaseID)
		if tenantID == 0 {
			tenantID = req.TenantID
		}
		chunks, err := s.chunks.ListChunksByKnowledgeIDAndTypes(
			ctx, tenantID, candidate.KnowledgeID, []types.ChunkType{types.ChunkTypeSheetEntityIndex},
		)
		if err != nil {
			return Result{}, err
		}
		if len(chunks) == 0 {
			continue
		}
		schemas := make([]spreadsheet.Schema, 0, len(chunks))
		for _, chunk := range chunks {
			if chunk == nil || !chunk.IsEnabled {
				continue
			}
			var schema spreadsheet.Schema
			if err := json.Unmarshal([]byte(chunk.Content), &schema); err != nil {
				continue
			}
			schemas = append(schemas, schema)
		}
		if len(schemas) == 0 {
			continue
		}
		sourceFile := strings.TrimSpace(candidate.KnowledgeFilename)
		if sourceFile == "" {
			sourceFile = candidate.KnowledgeTitle
		}
		executed := spreadsheet.Execute(req.QueryType, req.Query, sourceFile, schemas)
		if !executed.Completeness.IsExhaustive || strings.TrimSpace(executed.Content) == "" {
			continue
		}
		return Result{
			Found:        true,
			Content:      executed.Content,
			Completeness: executed.Completeness,
			Source:       candidate,
		}, nil
	}
	return Result{Completeness: types.PartialRetrievalCompleteness()}, nil
}
