package service

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

type exactResumeChunkKey struct {
	Kind  string
	Index int
}

func exactPersonalStorageCharge(ctx context.Context, size int64) int64 {
	if consumer, exact := exactPersonalConsumerFrom(ctx); exact && consumer.resuming {
		return size - consumer.resumeStorage
	}
	return size
}

// Only IDs and scheduling metadata are read from old output. Bodies come from
// the freshly checked original file through the ordinary parser and chunker.
func (c *exactPersonalConsumer) reproveResumeChunks(
	ctx context.Context, generated []*types.Chunk,
) error {
	if !c.resuming || c.parsed == nil || len(c.parsed.assets) != 0 || len(generated) == 0 {
		return errPersonalExactDocument
	}
	db := c.recovery.service.db.WithContext(ctx)
	var existing []struct {
		ID, ChunkType string
		ChunkIndex    int
	}
	if err := db.Table("chunks").Select("id,chunk_type,chunk_index").
		Where("knowledge_id=?", c.source.knowledge).Limit(2049).Scan(&existing).Error; err != nil {
		return err
	}
	if len(existing) != len(generated) || len(existing) > 2048 {
		return errPersonalExactDocument
	}
	ids := make(map[exactResumeChunkKey]string, len(existing))
	for _, row := range existing {
		key := exactResumeChunkKey{row.ChunkType, row.ChunkIndex}
		if (row.ChunkType != types.ChunkTypeText && row.ChunkType != types.ChunkTypeParentText) ||
			ids[key] != "" {
			return errPersonalExactDocument
		}
		ids[key] = row.ID
	}
	remap := make(map[string]string, len(generated))
	for _, chunk := range generated {
		id := ids[exactResumeChunkKey{chunk.ChunkType, chunk.ChunkIndex}]
		if id == "" || remap[chunk.ID] != "" {
			return errPersonalExactDocument
		}
		remap[chunk.ID] = id
	}
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(&types.Chunk{}); err != nil {
		return err
	}
	textIDs := make([]string, 0, len(generated))
	for _, chunk := range generated {
		chunk.ID = remap[chunk.ID]
		for _, link := range []*string{&chunk.ParentChunkID, &chunk.PreChunkID, &chunk.NextChunkID} {
			if *link != "" {
				id := remap[*link]
				if id == "" {
					return errPersonalExactDocument
				}
				*link = id
			}
		}
		chunk.Content = common.CleanInvalidUTF8(chunk.Content)
		chunk.ContextHeader = common.CleanInvalidUTF8(chunk.ContextHeader)
		chunk.IndexStatus = "ready"
		if chunk.Flags == 0 {
			chunk.Flags = types.ChunkFlagRecommended
		}
		candidate := *chunk
		candidate.IsEnabled = false
		columns := make(map[string]any)
		for _, field := range statement.Schema.Fields {
			if field.DBName == "" || field.DBName == "seq_id" ||
				field.DBName == "created_at" || field.DBName == "updated_at" {
				continue
			}
			value, _ := field.ValueOf(ctx, reflect.ValueOf(&candidate))
			switch typed := value.(type) {
			case types.SourceLocators:
				if len(typed) == 0 {
					value = nil
				}
			case gorm.DeletedAt:
				if typed.Valid {
					return errPersonalExactDocument
				}
				value = nil
			}
			columns[field.DBName] = value
		}
		canonical, err := json.Marshal(columns)
		if err != nil {
			return err
		}
		var equal bool
		if err := db.Raw(`SELECT EXISTS(SELECT 1 FROM chunks c WHERE c.id=?
AND (to_jsonb(c)-'created_at'-'updated_at'-'seq_id')=CAST(? AS jsonb))`,
			chunk.ID, string(canonical)).Scan(&equal).Error; err != nil {
			return err
		}
		if !equal {
			return errPersonalExactDocument
		}
		if chunk.ChunkType == types.ChunkTypeText {
			textIDs = append(textIDs, chunk.ID)
		}
	}
	var matching, total int64
	if err := db.Table("embeddings").Where("knowledge_id=?", c.source.knowledge).
		Count(&total).Error; err != nil {
		return err
	}
	if err := db.Table("embeddings").Where(
		"knowledge_id=? AND knowledge_base_id=? AND source_id=chunk_id AND source_id IN ?",
		c.source.knowledge, c.source.kb, textIDs).Count(&matching).Error; err != nil {
		return err
	}
	if matching != int64(len(textIDs)) || matching != total {
		return errPersonalExactDocument
	}
	// No old vector is trusted here. The normal indexer obtains fresh model
	// results for these exact IDs; final publication verifies those responses.
	return nil
}
