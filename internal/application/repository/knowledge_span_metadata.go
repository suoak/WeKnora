package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"

	"github.com/Tencent/WeKnora/internal/types"
)

var processingSpanName = regexp.MustCompile(
	`^(docreader|chunking|embedding|multimodal|postprocess)` +
		`(\.(image|batch|summary|question|wiki|graph\.chunk)(\[[0-9]+\])?)?$`,
)

// Status observers do not receive provider messages, source text, arbitrary
// span names/IDs or input/output/metadata. Parent links remain stable via hashes.
func projectProcessingSpanMetadata(rows []types.KnowledgeProcessingSpan) {
	id := func(value string) string {
		if value == "" {
			return ""
		}
		hash := sha256.Sum256([]byte(value))
		return hex.EncodeToString(hash[:])
	}
	for i := range rows {
		row := &rows[i]
		row.SpanID, row.ParentSpanID = id(row.SpanID), id(row.ParentSpanID)
		if row.Kind == types.SpanKindRoot {
			row.Name = "knowledge_processing"
		} else if !processingSpanName.MatchString(row.Name) {
			row.Name = "processing"
		}
		row.Input, row.Output, row.Metadata = nil, nil, nil
		row.ErrorMessage, row.ErrorDetail = "", ""
		if row.ErrorCode != "TASK_STALLED" {
			row.ErrorCode = ""
		}
	}
}
