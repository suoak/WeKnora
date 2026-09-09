package service

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/infrastructure/chunker"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type parentChildKnowledgeRepo struct {
	interfaces.KnowledgeRepository
	knowledge *types.Knowledge
}

func (r *parentChildKnowledgeRepo) GetKnowledgeByID(
	context.Context, uint64, string,
) (*types.Knowledge, error) {
	return r.knowledge, nil
}

func (r *parentChildKnowledgeRepo) UpdateKnowledge(
	context.Context, *types.Knowledge,
) error {
	return nil
}

type parentChildChunkService struct {
	interfaces.ChunkService
	created []*types.Chunk
}

func (s *parentChildChunkService) CreateChunks(_ context.Context, chunks []*types.Chunk) error {
	s.created = append([]*types.Chunk(nil), chunks...)
	return nil
}

type parentChildChunkRepository struct {
	interfaces.ChunkRepository
	created []*types.Chunk
}

func (r *parentChildChunkRepository) CreateChunks(_ context.Context, chunks []*types.Chunk) error {
	r.created = append([]*types.Chunk(nil), chunks...)
	return nil
}

type parentChildModelService struct {
	interfaces.ModelService
	embedder embedding.Embedder
}

func (s parentChildModelService) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return s.embedder, nil
}

type parentChildEmbedder struct{}

func (parentChildEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1}, nil
}

func (parentChildEmbedder) BatchEmbed(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1}}, nil
}

func (parentChildEmbedder) BatchEmbedWithPool(
	context.Context, embedding.Embedder, []string,
) ([][]float32, error) {
	return [][]float32{{1}}, nil
}

func (parentChildEmbedder) GetModelName() string { return "parent-child-test" }
func (parentChildEmbedder) GetDimensions() int   { return 1 }
func (parentChildEmbedder) GetModelID() string   { return "parent-child-test" }

type parentChildRetrieveEngine struct {
	interfaces.RetrieveEngineService
	indexed []*types.IndexInfo
}

func (e *parentChildRetrieveEngine) EngineType() types.RetrieverEngineType {
	return types.PostgresRetrieverEngineType
}

func (e *parentChildRetrieveEngine) Support() []types.RetrieverType {
	return []types.RetrieverType{types.VectorRetrieverType}
}

func (e *parentChildRetrieveEngine) DeleteByKnowledgeIDList(
	context.Context, []string, int, string,
) error {
	return nil
}

func (e *parentChildRetrieveEngine) EstimateStorageSize(
	context.Context, embedding.Embedder, []*types.IndexInfo, []types.RetrieverType,
) int64 {
	return 0
}

func (e *parentChildRetrieveEngine) BatchIndex(
	_ context.Context,
	_ embedding.Embedder,
	infos []*types.IndexInfo,
	_ []types.RetrieverType,
) error {
	e.indexed = append([]*types.IndexInfo(nil), infos...)
	return nil
}

type parentChildRetrieveRegistry struct {
	interfaces.RetrieveEngineRegistry
	engine interfaces.RetrieveEngineService
}

func (r parentChildRetrieveRegistry) GetRetrieveEngineService(
	types.RetrieverEngineType,
) (interfaces.RetrieveEngineService, error) {
	return r.engine, nil
}

type parentChildGraphRepo struct {
	interfaces.RetrieveGraphRepository
}

func (parentChildGraphRepo) DelGraph(context.Context, []types.NameSpace) error {
	return nil
}

type parentChildTenantRepo struct {
	interfaces.TenantRepository
}

func (parentChildTenantRepo) AdjustStorageUsed(context.Context, uint64, int64) error {
	return nil
}

type parentChildTaskEnqueuer struct{}

func (parentChildTaskEnqueuer) Enqueue(*asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	return nil, nil
}

func TestProcessChunksIndexesEveryTextChild(t *testing.T) {
	knowledge := &types.Knowledge{
		ID:              "knowledge-1",
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		ParseStatus:     types.ParseStatusProcessing,
	}
	chunkService := &parentChildChunkRepository{}
	retrieveEngine := &parentChildRetrieveEngine{}
	tenant := &types.Tenant{
		ID: 1,
		RetrieverEngines: types.RetrieverEngines{Engines: []types.RetrieverEngineParams{
			{
				RetrieverType:       types.VectorRetrieverType,
				RetrieverEngineType: types.PostgresRetrieverEngineType,
			},
		}},
	}
	ctx := context.WithValue(context.Background(), types.TenantInfoContextKey, tenant)
	svc := &knowledgeService{
		repo:           &parentChildKnowledgeRepo{knowledge: knowledge},
		chunkRepo:      chunkService,
		modelService:   parentChildModelService{embedder: parentChildEmbedder{}},
		retrieveEngine: parentChildRetrieveRegistry{engine: retrieveEngine},
		graphEngine:    parentChildGraphRepo{},
		tenantRepo:     parentChildTenantRepo{},
		task:           parentChildTaskEnqueuer{},
	}
	kb := &types.KnowledgeBase{
		ID:               "kb-1",
		TenantID:         1,
		EmbeddingModelID: "embedding-1",
		IndexingStrategy: types.IndexingStrategy{VectorEnabled: true},
	}
	chunks := []types.ParsedChunk{
		{Content: "linked child", Seq: 0, Start: 0, End: 12, ParentIndex: 0},
		{Content: "standalone child", Seq: 1, Start: 12, End: 28, ParentIndex: -1},
	}

	svc.processChunks(ctx, kb, knowledge, chunks, ProcessChunksOptions{
		ParentChunks: []types.ParsedParentChunk{
			{Content: "parent context", Seq: 0, Start: 0, End: 28},
		},
	})

	var textChunkIDs []string
	var linkedParentID string
	var standaloneParentID string
	for _, chunk := range chunkService.created {
		if chunk.ChunkType == types.ChunkTypeText {
			textChunkIDs = append(textChunkIDs, chunk.ID)
			if chunk.Content == "linked child" {
				linkedParentID = chunk.ParentChunkID
			}
			if chunk.Content == "standalone child" {
				standaloneParentID = chunk.ParentChunkID
			}
		}
	}
	require.Len(t, textChunkIDs, 2)
	require.NotEmpty(t, linkedParentID)
	require.Empty(t, standaloneParentID)

	indexedSourceIDs := make([]string, 0, len(retrieveEngine.indexed))
	for _, info := range retrieveEngine.indexed {
		indexedSourceIDs = append(indexedSourceIDs, info.SourceID)
	}
	require.ElementsMatch(t, textChunkIDs, indexedSourceIDs)
}

func TestProcessChunksPersistsWideParserRecordWithoutGenericSplitOrParent(t *testing.T) {
	row1 := "编号: 2024,三级规格: 最大并发连接数（IPv4+IPv6）," +
		strings.Repeat("MODEL-X: 50W,", 360) + "RG-NBR-N7204-E: 50W\n"
	row2 := "编号: 793,三级规格: 静态ARP数量\n"
	require.Greater(t, utf8.RuneCountInString(row1), 4000)
	readResult := preserveResult([]string{row1, row2})
	readResult.ParsedChunks[0].Metadata = map[string]string{
		"parser.source_kind": "excel_row",
		"parser.sheet":       "SPEC",
		"parser.row":         "2",
	}
	parsed, preserved, err := materializeParserDefinedChunks(readResult, 7500)
	require.NoError(t, err)
	require.True(t, preserved)

	knowledge := &types.Knowledge{
		ID: "knowledge-semantic", TenantID: 1, KnowledgeBaseID: "kb-1",
		ParseStatus: types.ParseStatusProcessing,
	}
	chunkService := &parentChildChunkService{}
	retrieveEngine := &parentChildRetrieveEngine{}
	tenant := &types.Tenant{ID: 1}
	ctx := context.WithValue(context.Background(), types.TenantInfoContextKey, tenant)
	svc := &knowledgeService{
		repo:           &parentChildKnowledgeRepo{knowledge: knowledge},
		chunkService:   chunkService,
		retrieveEngine: parentChildRetrieveRegistry{engine: retrieveEngine},
		graphEngine:    parentChildGraphRepo{},
		tenantRepo:     parentChildTenantRepo{},
		task:           parentChildTaskEnqueuer{},
	}
	kb := &types.KnowledgeBase{ID: "kb-1", TenantID: 1}

	svc.processChunks(ctx, kb, knowledge, parsed, ProcessChunksOptions{})

	var target *types.Chunk
	for _, chunk := range chunkService.created {
		if strings.Contains(chunk.Content, "RG-NBR-N7204-E: 50W") {
			target = chunk
			break
		}
	}
	require.NotNil(t, target)
	require.Contains(t, target.Content, "最大并发连接数（IPv4+IPv6）")
	require.NotContains(t, target.Content, "静态ARP数量")
	require.Empty(t, target.ParentChunkID)
	metadata, err := target.Metadata.Map()
	require.NoError(t, err)
	require.Equal(t, "excel_row", metadata["parser.source_kind"])
	require.Equal(t, "SPEC", metadata["parser.sheet"])
	require.Equal(t, "2", metadata["parser.row"])
}

func TestProcessChunksMixedSegmentsNeverLinksPreservedChunkToGenericParent(t *testing.T) {
	partA := strings.Repeat("alpha beta gamma delta ", 12)
	partB := "metric: complete semantic row\n"
	partC := strings.Repeat("one two three four five ", 12)
	content := partA + partB + partC
	startB := utf8.RuneCountInString(partA)
	startC := startB + utf8.RuneCountInString(partB)
	readResult := &types.ReadResult{
		MarkdownContent: content,
		ParsedSegments: []types.ParserDefinedSegment{
			{Seq: 0, Start: 0, End: startB, ChunkingPolicy: types.ChunkingPolicyDefault, Metadata: map[string]string{"parser.sheet": "A"}},
			{Seq: 1, Start: startB, End: startC, ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks, Metadata: map[string]string{"parser.sheet": "B"}, ParsedChunks: []types.ParserChunkSpan{{Seq: 0, Start: 0, End: utf8.RuneCountInString(partB), Metadata: map[string]string{"parser.sheet": "B", "parser.row": "2"}}}},
			{Seq: 2, Start: startC, End: utf8.RuneCountInString(content), ChunkingPolicy: types.ChunkingPolicyDefault, Metadata: map[string]string{"parser.sheet": "C"}},
		},
	}
	segments, segmented, err := materializeParserDefinedSegments(readResult, 1000)
	require.NoError(t, err)
	require.True(t, segmented)
	base := chunker.NormalizeSplitterConfig(chunker.SplitterConfig{
		ChunkSize: 30, ChunkOverlap: 5,
		Separators: []string{" "}, Strategy: chunker.StrategyLegacy,
	})
	parentCfg, childCfg := chunker.DeriveParentChildConfigs(base, 80, 25)
	parsed, parents, err := splitMaterializedParserSegments(
		segments, base, true, parentCfg, childCfg,
	)
	require.NoError(t, err)
	require.NotEmpty(t, parents)

	knowledge := &types.Knowledge{
		ID: "knowledge-mixed", TenantID: 1, KnowledgeBaseID: "kb-1",
		ParseStatus: types.ParseStatusProcessing,
	}
	chunkService := &parentChildChunkService{}
	ctx := context.WithValue(context.Background(), types.TenantInfoContextKey, &types.Tenant{ID: 1})
	svc := &knowledgeService{
		repo:           &parentChildKnowledgeRepo{knowledge: knowledge},
		chunkService:   chunkService,
		retrieveEngine: parentChildRetrieveRegistry{engine: &parentChildRetrieveEngine{}},
		graphEngine:    parentChildGraphRepo{},
		tenantRepo:     parentChildTenantRepo{},
		task:           parentChildTaskEnqueuer{},
	}
	svc.processChunks(ctx, &types.KnowledgeBase{ID: "kb-1", TenantID: 1}, knowledge, parsed,
		ProcessChunksOptions{ParentChunks: parents})

	parentSheetByID := make(map[string]string)
	for _, stored := range chunkService.created {
		if stored.ChunkType == types.ChunkTypeParentText {
			metadata, mapErr := stored.Metadata.Map()
			require.NoError(t, mapErr)
			parentSheetByID[stored.ID], _ = metadata["parser.sheet"].(string)
		}
	}
	seenB := false
	seenCLinked := false
	for _, stored := range chunkService.created {
		if stored.ChunkType != types.ChunkTypeText {
			continue
		}
		metadata, mapErr := stored.Metadata.Map()
		require.NoError(t, mapErr)
		sheet, _ := metadata["parser.sheet"].(string)
		switch sheet {
		case "B":
			seenB = true
			require.Empty(t, stored.ParentChunkID)
		case "C":
			if stored.ParentChunkID != "" {
				seenCLinked = true
				require.Equal(t, "C", parentSheetByID[stored.ParentChunkID])
			}
		}
	}
	require.True(t, seenB)
	require.True(t, seenCLinked)
}
