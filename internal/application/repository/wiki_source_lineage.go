package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// VerifiedPageLineage reads canonical reference metadata only after proving the
// exact page and its live input closure. The caller must revalidate that same
// page at the final output boundary, including after rendering link labels.
func (s *PersonalWikiStore) VerifiedPageLineage(
	ctx context.Context, tenant uint64, kb string, authorized []string, page *types.WikiPage,
) (*types.WikiPageLineage, error) {
	if err := s.RevalidatePages(ctx, tenant, kb, authorized, []*types.WikiPage{page}); err != nil {
		return nil, err
	}
	lineage := &types.WikiPageLineage{
		PageID: page.ID, RevisionSHA256: wikiPageDigest(page), Status: "complete",
		Sources: []types.WikiSourceAttribution{},
	}
	ids := make([]string, 0, len(page.SourceRefs))
	for _, ref := range page.SourceRefs {
		ids = append(ids, types.WikiSourceKnowledgeID(ref))
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	chunks := append([]string(nil), page.ChunkRefs...)
	slices.Sort(chunks)
	chunks = slices.Compact(chunks)
	lineage.SourceCount, lineage.ChunkCount = len(ids), len(chunks)
	// Do not return a silently truncated or synthesized attribution list.
	if len(ids) > 2048 || len(chunks) > 8192 {
		lineage.Status = "partial"
		return lineage, nil
	}
	db := s.DB.WithContext(ctx)
	var seal WikiMCPProvenance
	if err := db.Where("page_id=? AND tenant_id=? AND knowledge_base_id=?", page.ID, tenant, kb).
		Take(&seal).Error; err != nil {
		return nil, err
	}
	var versions map[string]string
	if seal.PageSHA256 != lineage.RevisionSHA256 || json.Unmarshal([]byte(seal.SourceVersions), &versions) != nil ||
		wikiDigest(versions) != seal.DependencySHA256 {
		return nil, ErrWikiMCPUnverified
	}
	byDocument := make(map[string]int, len(ids))
	for start := 0; start < len(ids); start += 200 {
		var documents []struct{ ID, Title string }
		// These are proof-authorized canonical metadata reads, not a general
		// repository escape hatch. Never load bodies or serialized source URLs.
		if err := db.Session(&gorm.Session{SkipHooks: true}).Table("knowledges AS lineage_documents").
			Select("id,title").Where("id IN ? AND tenant_id=? AND knowledge_base_id=? AND "+
			"deleted_at IS NULL AND enable_status='enabled' AND parse_status IN ('completed','finalizing')",
			ids[start:min(start+200, len(ids))], tenant, kb).Order("id").Find(&documents).Error; err != nil {
			return nil, err
		}
		for _, document := range documents {
			if versions[document.ID] == "" || !slices.Contains(authorized, document.ID) {
				return nil, ErrWikiMCPUnverified
			}
			byDocument[document.ID] = len(lineage.Sources)
			lineage.Sources = append(lineage.Sources, types.WikiSourceAttribution{
				KnowledgeID: document.ID, Title: document.Title, VersionSHA256: versions[document.ID],
				Reference: fmt.Sprintf("knowhub:tenant:%d:kb:%s:document:%s", tenant, kb, document.ID),
				Level:     "document", Chunks: []types.WikiChunkReference{},
			})
		}
	}
	if len(lineage.Sources) != len(ids) {
		return nil, ErrWikiMCPUnverified
	}
	validChunks := 0
	for start := 0; start < len(chunks); start += 200 {
		var references []struct{ ID, KnowledgeID string }
		if err := db.Session(&gorm.Session{SkipHooks: true}).Table("chunks AS lineage_chunks").
			Select("id,knowledge_id").Where("id IN ? AND tenant_id=? AND knowledge_base_id=? AND "+
			"deleted_at IS NULL AND is_enabled=true AND chunk_type=?",
			chunks[start:min(start+200, len(chunks))], tenant, kb, types.ChunkTypeText).
			Order("id").Find(&references).Error; err != nil {
			return nil, err
		}
		for _, reference := range references {
			index, ok := byDocument[reference.KnowledgeID]
			if !ok {
				continue // A signed stale/unrelated chunk ID is not a citation.
			}
			source := &lineage.Sources[index]
			source.Level = "chunk"
			source.Chunks = append(source.Chunks, types.WikiChunkReference{
				ChunkID: reference.ID, Reference: source.Reference + ":chunk:" + reference.ID,
			})
			validChunks++
		}
	}
	if validChunks != len(chunks) {
		lineage.Status = "partial"
	}
	return lineage, nil
}
