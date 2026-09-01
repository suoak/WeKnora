package service

import (
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/spreadsheet"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

// buildSpreadsheetEntityChunks materializes parser-owned sheet schemas as
// non-vector chunks. Existing row/text chunks remain the only embedding input.
func buildSpreadsheetEntityChunks(
	knowledge *types.Knowledge,
	metadata map[string]string,
	firstIndex int,
) ([]*types.Chunk, error) {
	schemas, err := spreadsheet.ParseSchemas(metadata[spreadsheet.SchemasMetadataKey])
	if err != nil {
		return nil, err
	}
	result := make([]*types.Chunk, 0, len(schemas))
	for _, schema := range schemas {
		if (schema.EntityAxis != "columns" && schema.EntityAxis != "rows") ||
			schema.Confidence <= 0 || len(schema.EntityHeaders) == 0 {
			continue
		}
		content, err := spreadsheet.EncodeSchema(schema)
		if err != nil {
			return nil, err
		}
		chunkMetadata, err := json.Marshal(map[string]interface{}{
			"parser": map[string]string{"sheet": schema.SheetName},
			"spreadsheet": map[string]interface{}{
				"entity_axis":  schema.EntityAxis,
				"entity_count": len(schema.EntityHeaders),
				"confidence":   schema.Confidence,
			},
		})
		if err != nil {
			return nil, err
		}
		now := time.Now()
		result = append(result, &types.Chunk{
			ID:              uuid.New().String(),
			TenantID:        knowledge.TenantID,
			KnowledgeID:     knowledge.ID,
			KnowledgeBaseID: knowledge.KnowledgeBaseID,
			Content:         content,
			SourceContent:   content,
			ChunkIndex:      firstIndex + len(result),
			IsEnabled:       true,
			Status:          int(types.ChunkStatusStored),
			ChunkType:       types.ChunkTypeSheetEntityIndex,
			Metadata:        types.JSON(chunkMetadata),
			CreatedAt:       now,
			UpdatedAt:       now,
		})
	}
	return result, nil
}
