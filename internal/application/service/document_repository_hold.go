package service

import (
	"context"
	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Raw repository escape hatches remain HOLD while protected business reads are enabled.
// Knowledge reads are rejected here; certified Chunk reads below delegate back
// to the protected service. Embedding retains existing write admission behavior.
type heldDocumentKnowledgeRepository struct{ interfaces.KnowledgeRepository }

func (r *heldDocumentKnowledgeRepository) GetKnowledgeByID(ctx context.Context, tenantID uint64, id string) (*types.Knowledge, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) GetKnowledgeByIDOnly(ctx context.Context, id string) (*types.Knowledge, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) ListKnowledgeByKnowledgeBaseID(ctx context.Context, tenantID uint64, kbID string) ([]*types.Knowledge, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) ListPagedKnowledgeByKnowledgeBaseID(ctx context.Context,
	tenantID uint64, kbID string, page *types.Pagination, filter types.KnowledgeListFilter,
) ([]*types.Knowledge, int64, error) {
	return nil, 0, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) GetKnowledgeBatch(ctx context.Context, tenantID uint64, ids []string) ([]*types.Knowledge, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) GetKnowledgeBatchByIDOnly(ctx context.Context, ids []string) ([]*types.Knowledge, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) CheckKnowledgeExists(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	params *types.KnowledgeCheckParams,
) (bool, *types.Knowledge, error) {
	return false, nil, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) ListKnowledgeFolderCounts(
	ctx context.Context,
	tenantID uint64,
	kbID string,
) ([]*types.KnowledgeFolderCount, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) CountKnowledgeByKnowledgeBaseID(ctx context.Context, tenantID uint64, kbID string) (int64, error) {
	return 0, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) CountKnowledgeByStatus(ctx context.Context, tenantID uint64, kbID string, parseStatuses []string) (int64, error) {
	return 0, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) SearchKnowledge(ctx context.Context, tenantID uint64, keyword string, offset, limit int, fileTypes []string) ([]*types.Knowledge, bool, error) {
	return nil, false, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) FindByMetadataKey(ctx context.Context, tenantID uint64, kbID string, key string, value string) (*types.Knowledge, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) FindByMetadataKeyPrefix(ctx context.Context, tenantID uint64, kbID string, key string, prefix string) ([]*types.Knowledge, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) FindByDataSourceExternalID(
	ctx context.Context, tenantID uint64, kbID, dataSourceID, externalID string,
) (*types.Knowledge, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) SearchKnowledgeInScopes(ctx context.Context, scopes []types.KnowledgeSearchScope, keyword string, offset, limit int, fileTypes []string) ([]*types.Knowledge, bool, int64, error) {
	return nil, false, 0, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) ListIDsByTagIDs(ctx context.Context, tenantID uint64, kbID string, tagIDs []string) ([]string, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) GetKnowledgeTags(ctx context.Context, knowledgeIDs []string) (map[string][]*types.KnowledgeTag, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentKnowledgeRepository) ListKnowledgeProfileRows(ctx context.Context, tenantID uint64, kbID string) ([]*types.KnowledgeProfileRow, error) {
	return nil, access.ErrForbidden
}

type heldDocumentChunkRepository struct {
	interfaces.ChunkRepository
	service *chunkService
}

func (r *heldDocumentChunkRepository) ListAllChunksByKnowledgeID(ctx context.Context, tenantID uint64, knowledgeID string) ([]*types.Chunk, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) GetChunkByID(ctx context.Context, tenantID uint64, id string) (*types.Chunk, error) {
	if r.service == nil {
		return nil, access.ErrForbidden
	}
	row, err := r.service.readAuthorizedChunk(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.TenantID != tenantID {
		return nil, access.ErrForbidden
	}
	return row, nil
}
func (r *heldDocumentChunkRepository) GetChunkByIDOnly(ctx context.Context, id string) (*types.Chunk, error) {
	if r.service == nil {
		return nil, access.ErrForbidden
	}
	return r.service.readAuthorizedChunk(ctx, id)
}
func (r *heldDocumentChunkRepository) GetChunkBySeqID(ctx context.Context, tenantID uint64, seqID int64) (*types.Chunk, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListChunksByID(ctx context.Context, tenantID uint64, ids []string) ([]*types.Chunk, error) {
	return r.readBatch(ctx, tenantID, ids)
}
func (r *heldDocumentChunkRepository) ListChunksByIDOnly(ctx context.Context, ids []string) ([]*types.Chunk, error) {
	return r.readBatch(ctx, 0, ids)
}
func (r *heldDocumentChunkRepository) ListChunksBySeqID(ctx context.Context, tenantID uint64, seqIDs []int64) ([]*types.Chunk, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListChunksByKnowledgeID(ctx context.Context, tenantID uint64, knowledgeID string) ([]*types.Chunk, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListChunksByKnowledgeIDAndTypes(
	ctx context.Context, tenantID uint64, knowledgeID string, chunkTypes []types.ChunkType,
) ([]*types.Chunk, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListPagedChunksByKnowledgeID(
	ctx context.Context,
	tenantID uint64,
	knowledgeID string,
	page *types.Pagination,
	chunkType []types.ChunkType,
	tagIDs []string,
	keyword string,
	searchField string,
	sortOrder string,
	knowledgeType string,
	isEnabled *bool,
) ([]*types.Chunk, int64, error) {
	return nil, 0, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListChunkByParentID(ctx context.Context, tenantID uint64, parentID string) ([]*types.Chunk, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListChunkNeighbors(
	ctx context.Context,
	tenantID uint64,
	knowledgeID string,
	chunkIndex int,
	before int,
	after int,
	chunkTypes []types.ChunkType,
) ([]*types.Chunk, error) {
	if r.service == nil || before < 0 || after < 0 || before > 32 || after > 32 {
		return nil, access.ErrForbidden
	}
	for _, kind := range chunkTypes {
		if kind != types.ChunkTypeText {
			return nil, access.ErrForbidden
		}
	}
	doc, grant, err := authorizeStoredDocument(ctx, r.service.documentACL, r.service.knowledgeRepo, knowledgeID)
	if err != nil {
		return nil, err
	}
	if doc.TenantID != tenantID {
		return nil, access.ErrForbidden
	}
	rows, err := r.ChunkRepository.ListChunkNeighbors(ctx, tenantID, knowledgeID, chunkIndex, before, after, chunkTypes)
	if err != nil {
		return nil, err
	}
	out := make([]*types.Chunk, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.KnowledgeID != doc.ID || row.KnowledgeBaseID != doc.KnowledgeBaseID || row.TenantID != doc.TenantID {
			return nil, access.ErrForbidden
		}
		safe, err := r.service.readAuthorizedChunk(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, safe)
	}
	if err = grant.RetainForOutput(ctx); err != nil {
		return nil, err
	}
	return out, nil

}
func (r *heldDocumentChunkRepository) ListChunksByParentIDs(ctx context.Context, tenantID uint64, parentIDs []string) ([]*types.Chunk, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListChunksByParentIDsOnly(ctx context.Context, parentIDs []string) ([]*types.Chunk, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListChunkRevisions(ctx context.Context, tenantID uint64, chunkID string) ([]*types.ChunkRevision, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) GetChunkRevision(ctx context.Context, tenantID uint64, chunkID string, revision int) (*types.ChunkRevision, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListImageInfoByKnowledgeIDs(ctx context.Context, tenantID uint64, knowledgeIDs []string) ([]interfaces.ChunkImageInfo, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListImageAssets(
	ctx context.Context, tenantID uint64, kbID string, q *types.ImageAssetQuery,
) ([]types.ImageAssetRow, int64, error) {
	return nil, 0, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) CountChunksByKnowledgeBaseID(ctx context.Context, tenantID uint64, kbID string) (int64, error) {
	return 0, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListAllFAQChunksByKnowledgeID(ctx context.Context, tenantID uint64, knowledgeID string) ([]*types.Chunk, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListAllFAQChunksWithMetadataByKnowledgeBaseID(ctx context.Context, tenantID uint64, kbID string) ([]*types.Chunk, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) FindFAQChunkWithDuplicateQuestion(ctx context.Context, tenantID uint64, kbID string, excludeChunkID string, questions []string) (*types.Chunk, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListAllFAQChunksForExport(ctx context.Context, tenantID uint64, knowledgeID string) ([]*types.Chunk, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) FAQChunkDiff(ctx context.Context, srcTenantID uint64, srcKBID string, dstTenantID uint64, dstKBID string) (*types.FAQChunkDiffResult, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListFAQChunkStatusByIDs(ctx context.Context, tenantID uint64, ids []string) (map[string]*types.FAQChunkStatus, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListRecommendedFAQChunks(ctx context.Context, tenantID uint64, kbIDs []string, knowledgeIDs []string, tagIDs []string, limit int) ([]*types.Chunk, error) {
	return nil, access.ErrForbidden
}
func (r *heldDocumentChunkRepository) ListRecentDocumentChunksWithQuestions(ctx context.Context, tenantID uint64, kbIDs []string, knowledgeIDs []string, limit int) ([]*types.Chunk, error) {
	return nil, access.ErrForbidden
}

func (r *heldDocumentChunkRepository) readBatch(ctx context.Context, tenant uint64, ids []string) ([]*types.Chunk, error) {
	if r.service == nil || len(ids) > access.MaxDocumentAllowSet {
		return nil, access.ErrForbidden
	}
	principal, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	if !ok || principal.Type != types.PrincipalWebUser || principal.ID == "" || types.CallerFromContext(ctx).UserID != principal.ID {
		return nil, access.ErrUnauthorized
	}
	out := make([]*types.Chunk, 0, len(ids))
	grants := []*access.DocumentReadGrant{}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			return nil, access.ErrForbidden
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		row, grant, err := r.service.readAuthorizedChunkWithGrant(ctx, id)
		if err != nil {
			return nil, err
		}
		if tenant != 0 && row.TenantID != tenant {
			return nil, access.ErrForbidden
		}
		out = append(out, row)
		grants = append(grants, grant)
	}
	for _, grant := range grants {
		if err := grant.RetainForOutput(ctx); err != nil {
			return nil, err
		}
	}
	return out, nil
}
