package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

// Checkpoint metadata lives in the private staging receipt, never in a public
// publication receipt or a new queue. Publish replaces it atomically. Keeping
// the manifest schema unchanged also preserves old-binary rollback reads.
type legacyEmbeddingProgress struct {
	Checkpoint *types.FeishuLegacyEmbeddingCheckpoint `json:"embedding_checkpoint_v1"`
}

// LegacyEmbeddingInputHash identifies the exact text sent to the model.
func LegacyEmbeddingInputHash(input string) string {
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}

func embeddingCheckpointHash(c types.FeishuLegacyEmbeddingCheckpoint) string {
	c.SHA256 = ""
	b, _ := json.Marshal(c)
	return LegacyEmbeddingInputHash(string(b))
}

func embeddingCheckpoint(a *types.FeishuLegacyAttempt) (*types.FeishuLegacyEmbeddingCheckpoint, error) {
	var p legacyEmbeddingProgress
	if json.Unmarshal(a.Receipt, &p) != nil || p.Checkpoint == nil {
		return nil, legacyFence("embedding_checkpoint_missing")
	}
	c := p.Checkpoint
	if c.Version != 1 || c.ResourceID != a.ResourceID || c.SourceHash != a.SourceHash ||
		c.SHA256 != embeddingCheckpointHash(*c) || c.Dimension < 1 || c.Dimension > 4096 ||
		len(c.Inputs) == 0 || len(c.Inputs) > 10000 {
		return nil, legacyFence("embedding_checkpoint_invalid")
	}
	return c, nil
}

// StartEmbeddingCheckpoint registers inputs under the current execution fence.
func (s *FeishuLegacyStore) StartEmbeddingCheckpoint(
	ctx context.Context, ds *types.DataSource, owner *types.FeishuLegacyAttempt,
	c types.FeishuLegacyEmbeddingCheckpoint,
) error {
	if c.ModelID == "" || c.ModelStamp == "" || len(c.KBConfigSHA256) != 64 ||
		c.Dimension < 1 || c.Dimension > 4096 || len(c.Inputs) == 0 || len(c.Inputs) > 10000 {
		return legacyFence("embedding_checkpoint_shape")
	}
	seen := map[string]bool{}
	for _, input := range c.Inputs {
		digest, decodeErr := hex.DecodeString(input.SHA256)
		if input.ChunkID == "" || len(input.ChunkID) > 36 || seen[input.ChunkID] ||
			decodeErr != nil || len(digest) != 32 {
			return legacyFence("embedding_checkpoint_input")
		}
		seen[input.ChunkID] = true
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, a, err := s.owned(ctx, tx, ds, owner)
		if err != nil {
			return err
		}
		if a.Status != "staging" {
			return legacyFence("attempt_not_staging")
		}
		c.Version, c.ResourceID, c.SourceHash = 1, a.ResourceID, a.SourceHash
		c.SHA256 = embeddingCheckpointHash(c)
		var envelope map[string]json.RawMessage
		if json.Unmarshal(a.Receipt, &envelope) == nil && envelope["embedding_checkpoint_v1"] != nil {
			prior, e := embeddingCheckpoint(a)
			if e != nil {
				return e
			}
			if prior.SHA256 != c.SHA256 {
				return legacyFence("embedding_checkpoint_changed")
			}
		}
		b, err := json.Marshal(legacyEmbeddingProgress{Checkpoint: &c})
		if err != nil {
			return err
		}
		return tx.Model(a).Update("receipt", json.RawMessage(b)).Error
	})
}

func validCheckpointVector(v []float32, dimension int) bool {
	if len(v) != dimension {
		return false
	}
	norm := 0.0
	for _, x := range v {
		n := float64(x)
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 65504 {
			return false
		}
		norm += n * n
	}
	return norm > 0
}

// SaveEmbeddingCheckpoint runs after a validated physical HTTP response. Even
// if a later SDK sub-batch fails, these private rows survive the retry. Every
// write still owns the current SQL execution, resource generation and nonce.
func (s *FeishuLegacyStore) SaveEmbeddingCheckpoint(
	ctx context.Context, ds *types.DataSource, owner *types.FeishuLegacyAttempt,
	inputs []string, vectors [][]float32,
) error {
	if len(inputs) == 0 || len(inputs) > 20 || len(inputs) != len(vectors) {
		return legacyFence("embedding_checkpoint_batch_shape")
	}
	if err := types.CheckFeishuSourceRead(ctx); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, a, err := s.owned(ctx, tx, ds, owner)
		if err != nil {
			return err
		}
		if a.Status != "staging" {
			return legacyFence("attempt_not_staging")
		}
		c, err := embeddingCheckpoint(a)
		if err != nil {
			return err
		}
		byHash := map[string][]string{}
		for _, input := range c.Inputs {
			byHash[input.SHA256] = append(byHash[input.SHA256], input.ChunkID)
		}
		for i, input := range inputs {
			ids := byHash[LegacyEmbeddingInputHash(input)]
			if len(ids) == 0 || !validCheckpointVector(vectors[i], c.Dimension) {
				return legacyFence("embedding_checkpoint_response_mismatch")
			}
			for _, id := range ids {
				result := tx.Exec(`INSERT INTO feishu_legacy_index_candidates AS candidate
(attempt_id,chunk_id,content,dimension,embedding) VALUES(?,?,?,?,?)
ON CONFLICT(attempt_id,chunk_id) DO UPDATE SET chunk_id=EXCLUDED.chunk_id
WHERE candidate.content=EXCLUDED.content AND candidate.dimension=EXCLUDED.dimension
AND candidate.embedding=EXCLUDED.embedding`, a.ID, id, input, c.Dimension, pgvector.NewHalfVector(vectors[i]))
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return legacyFence("embedding_checkpoint_conflict")
				}
			}
		}
		return nil
	})
}

type legacyCheckpointRow struct {
	ChunkID   string
	Content   string
	Dimension int
	Embedding pgvector.HalfVector
}

// ReusableEmbeddingCheckpoint returns vectors only for freshly registered
// exact input hashes in this resource. Old untyped staging records are never
// certified. Changed source bodies are parsed afresh; no old body is returned.
func (s *FeishuLegacyStore) ReusableEmbeddingCheckpoint(
	ctx context.Context, ds *types.DataSource, owner *types.FeishuLegacyAttempt,
) (map[string][]float32, error) {
	if err := types.CheckFeishuSourceRead(ctx); err != nil {
		return nil, err
	}
	out := map[string][]float32{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		r, a, err := s.owned(ctx, tx, ds, owner)
		if err != nil {
			return err
		}
		current, err := embeddingCheckpoint(a)
		if err != nil {
			return err
		}
		var initial types.FeishuLegacyManifest
		if json.Unmarshal(a.Manifest, &initial) != nil {
			return legacyFence("embedding_checkpoint_manifest")
		}
		// One most-complete private checkpoint plus the latest publication cover
		// a resumed new revision and its unchanged, previously published inputs.
		var candidates []types.FeishuLegacyAttempt
		if err = tx.Raw(`(SELECT old.* FROM feishu_legacy_attempts old
WHERE old.resource_id=? AND old.status='published' ORDER BY old.generation DESC LIMIT 1)
UNION ALL
(SELECT old.* FROM feishu_legacy_attempts old
WHERE old.resource_id=? AND old.status IN ('staging','ready','obsolete')
AND old.receipt->'embedding_checkpoint_v1'->>'model_stamp'=?
AND old.receipt->'embedding_checkpoint_v1'->>'model_id'=?
AND old.receipt->'embedding_checkpoint_v1'->>'kb_config_sha256'=?
ORDER BY (SELECT count(*) FROM feishu_legacy_index_candidates i WHERE i.attempt_id=old.id) DESC,
old.generation DESC LIMIT 1)`, r.ID, r.ID, current.ModelStamp, current.ModelID,
			current.KBConfigSHA256).Scan(&candidates).Error; err != nil {
			return err
		}
		candidates = append(candidates, *a)
		wanted := map[string]bool{}
		for _, input := range current.Inputs {
			wanted[input.SHA256] = true
		}
		for _, previous := range candidates {
			var m types.FeishuLegacyManifest
			if json.Unmarshal(previous.Manifest, &m) != nil {
				continue
			}
			if m.SourceIdentity != nil {
				hash, e := LegacyManifestHash(m)
				if e != nil || hash != m.SHA256 {
					continue
				}
			}
			expected := map[string]string{}
			if previous.Status == "published" {
				hash, e := LegacyManifestHash(m)
				var receipt types.FeishuLegacyReceipt
				if e != nil || hash != m.SHA256 || json.Unmarshal(previous.Receipt, &receipt) != nil ||
					receipt.ManifestSHA256 != m.SHA256 || receipt.ResourceID != r.ID ||
					receipt.AttemptID != previous.ID || receipt.Generation != previous.Generation ||
					receipt.KnowledgeID != r.KnowledgeID || m.EmbeddingModelID != current.ModelID ||
					m.Knowledge.ID != r.KnowledgeID || m.Knowledge.TenantID != r.TenantID ||
					m.Knowledge.KnowledgeBaseID != r.KnowledgeBaseID ||
					m.ModelStamp != current.ModelStamp || m.KBConfigSHA256 != current.KBConfigSHA256 ||
					m.Dimension != current.Dimension {
					continue
				}
				indexed, e := legacyIndexedChunks(m.Chunks)
				if e != nil {
					continue
				}
				for _, chunk := range indexed {
					chunk.ContextHeader = m.ChunkHeaders[chunk.ID]
					expected[chunk.ID] = LegacyEmbeddingInputHash(
						buildLegacyIndexContent(m.Knowledge.Title, chunk.EmbeddingContent()))
				}
			} else {
				c, e := embeddingCheckpoint(&previous)
				if e != nil || c.ModelID != current.ModelID || c.ModelStamp != current.ModelStamp ||
					c.KBConfigSHA256 != current.KBConfigSHA256 || c.Dimension != current.Dimension {
					continue
				}
				// A changed spreadsheet revision must not discard identical model work.
				// These are private typed model-response checkpoints for this same resource,
				// not publication receipts. Only hashes freshly registered by the current
				// authorized execution can be returned below; old bodies are never reused.
				for _, input := range c.Inputs {
					expected[input.ChunkID] = input.SHA256
				}
			}
			var rows []legacyCheckpointRow
			if err = tx.Table("feishu_legacy_index_candidates").Where("attempt_id=?", previous.ID).
				Limit(10001).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) > 10000 {
				return legacyFence("embedding_checkpoint_rows_exceeded")
			}
			for _, row := range rows {
				digest := LegacyEmbeddingInputHash(row.Content)
				vector := row.Embedding.Slice()
				if wanted[digest] && expected[row.ChunkID] == digest && row.Dimension == current.Dimension &&
					validCheckpointVector(vector, current.Dimension) {
					out[digest] = append([]float32(nil), vector...)
				}
			}
		}
		return nil
	})
	return out, err
}

func validateEmbeddingCheckpointForPrepare(
	a *types.FeishuLegacyAttempt, m *types.FeishuLegacyManifest, indexed []*types.Chunk,
) (bool, error) {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(a.Receipt, &envelope) != nil || envelope["embedding_checkpoint_v1"] == nil {
		return false, nil
	}
	c, err := embeddingCheckpoint(a)
	if err != nil {
		return false, err
	}
	if c.ModelID != m.EmbeddingModelID || c.ModelStamp != m.ModelStamp ||
		c.KBConfigSHA256 != m.KBConfigSHA256 || c.Dimension != m.Dimension || len(c.Inputs) != len(indexed) {
		return false, legacyFence("embedding_checkpoint_publication_binding")
	}
	for i, chunk := range indexed {
		chunk.ContextHeader = m.ChunkHeaders[chunk.ID]
		if c.Inputs[i].ChunkID != chunk.ID || c.Inputs[i].SHA256 !=
			LegacyEmbeddingInputHash(buildLegacyIndexContent(m.Knowledge.Title, chunk.EmbeddingContent())) {
			return false, legacyFence("embedding_checkpoint_publication_input")
		}
	}
	return true, nil
}
