package repository

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

const wikiNoWikiReason = "insufficient_plain_text_content"

// Certify only a complete single plain-text chunk. Images, OCR, captions,
// headers, child chunks and unknown field representations remain on the normal
// generation path, even when their raw text happens to be short.
func wikiInsufficientPlainText(inputs []map[string]interface{}) (int, bool) {
	if len(inputs) != 1 {
		return 0, false
	}
	row := inputs[0]
	for _, key := range []string{"context_header", "parent_chunk_id", "image_info"} {
		if row[key] != nil && row[key] != "" {
			return 0, false
		}
	}
	enabled := row["is_enabled"] == true || row["is_enabled"] == int64(1)
	if row["deleted_at"] != nil || !enabled {
		return 0, false
	}
	if row["chunk_type"] != "text" && row["chunk_type"] != "" {
		return 0, false
	}
	text, ok := row["content"].(string)
	if !ok || strings.ContainsAny(text, "`!<[") {
		return 0, false
	}
	n := utf8.RuneCountInString(strings.TrimSpace(text))
	return n, n > 0 && n < types.WikiMinimumTextRunes
}

type wikiNoWikiEvidence struct {
	KnowledgeID string    `json:"knowledge_id"`
	Reason      string    `json:"reason"`
	SourceSHA   string    `json:"source_sha256"`
	SourceEpoch string    `json:"source_epoch"`
	TextRunes   int       `json:"text_runes"`
	Minimum     int       `json:"minimum_text_runes"`
	CheckedAt   time.Time `json:"checked_at"`
}

// Called only by ordinary generation preflight after the complete current
// source/publication proof. The durable terminal does not alter the source,
// task attempts, checkpoints, or any historical page.
func recordWikiNoWikiSources(tx *gorm.DB, proof *wikiGenerationProof) error {
	for id, n := range proof.NoWikiSources {
		if proof.Sources[id] == "" || proof.Epochs[id] == "" || n < 1 || n >= types.WikiMinimumTextRunes {
			return ErrWikiMCPUnverified
		}
		if err := writeWikiDocumentSkip(tx, proof.Tenant, proof.KB, id, wikiNoWikiReason); err != nil {
			return err
		}
		evidence := wikiNoWikiEvidence{
			id, wikiNoWikiReason, proof.Sources[id], proof.Epochs[id], n,
			types.WikiMinimumTextRunes, time.Now().UTC(),
		}
		raw, err := json.Marshal(evidence)
		if err != nil {
			return err
		}
		result := tx.Model(&types.TaskPendingOp{}).Where("tenant_id=? AND scope=? AND scope_id=? AND "+
			"task_type='wiki:document_skip' AND dedup_key=?", proof.Tenant, wikiDocumentSkipScope, proof.KB, id).
			Updates(map[string]any{"op": "no_wiki", "payload": json.RawMessage(raw)})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrWikiMCPInputsChanged
		}
	}
	return nil
}

// A terminal row alone never supplies authority. Both its content digest and
// source epoch must match a newly verified current, still-insufficient source.
func verifiedWikiNoWikiSource(tx *gorm.DB, proof *wikiGenerationProof, id string) (bool, error) {
	n, insufficient := proof.NoWikiSources[id]
	if !insufficient {
		return false, nil
	}
	var rows []types.TaskPendingOp
	if err := tx.Where("tenant_id=? AND scope=? AND scope_id=? AND task_type='wiki:document_skip' "+
		"AND dedup_key=? AND op='no_wiki'", proof.Tenant, wikiDocumentSkipScope, proof.KB, id).Limit(2).
		Find(&rows).Error; err != nil {
		return false, err
	}
	if len(rows) != 1 {
		return false, nil
	}
	var evidence wikiNoWikiEvidence
	if json.Unmarshal(rows[0].Payload, &evidence) != nil {
		return false, nil
	}
	return evidence.KnowledgeID == id && evidence.Reason == wikiNoWikiReason &&
		evidence.SourceSHA != "" && evidence.SourceSHA == proof.Sources[id] &&
		evidence.SourceEpoch != "" && evidence.SourceEpoch == proof.Epochs[id] &&
		evidence.TextRunes == n && n > 0 && n < types.WikiMinimumTextRunes &&
		evidence.Minimum == types.WikiMinimumTextRunes, nil
}
