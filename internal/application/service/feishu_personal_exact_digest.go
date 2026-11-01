package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func exactChunkDigest(chunk *types.Chunk) ([32]byte, error) {
	if chunk == nil {
		return [32]byte{}, errPersonalExactDocument
	}
	canonicalChunk := *chunk
	// Keep every semantic field. Only database time/sequence allocation and
	// staging visibility are excluded, as in the original execution receipt.
	canonicalChunk.CreatedAt, canonicalChunk.UpdatedAt = time.Time{}, time.Time{}
	canonicalChunk.SeqID = 0
	canonicalChunk.IsEnabled = false
	data, err := json.Marshal(struct {
		Chunk         types.Chunk
		SourceContent string
	}{canonicalChunk, canonicalChunk.SourceContent})
	if err != nil {
		return [32]byte{}, err
	}
	// JSONB reorders RawMessage object keys when a chunk is read back.
	// Canonicalize the complete payload, retaining every field, JSON type and
	// array position. UseNumber avoids rounding metadata integers through
	// float64. This changes no authorization, generation or publication fence.
	var semantic any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&semantic); err != nil {
		return [32]byte{}, err
	}
	data, err = json.Marshal(semantic)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}
