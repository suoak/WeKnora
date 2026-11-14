package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

func exactPersonalFileFormat(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".docx":
		return "docx"
	case ".xlsx":
		return "xlsx"
	default:
		return ""
	}
}

func exactPersonalFormatEnabled(format string) bool {
	return format == "docx" || format == "xlsx" && repository.PersonalExactStandaloneXLSXEnabled()
}

func (p *PersonalRecovery) parseExactPersonalXLSX(
	ctx context.Context, source *personalExactDocument,
) (*personalExactParsedDocument, error) {
	workbook, err := parseExactStandaloneWorkbook(ctx, source.filename, source.file)
	if err != nil {
		return nil, err
	}
	parsed := &personalExactParsedDocument{
		source: source, assets: workbook.Assets, parserVersion: exactStandaloneXLSXVersion,
	}
	var text strings.Builder
	position := 0
	for _, section := range workbook.Sections {
		length := utf8.RuneCountInString(section.markdown)
		metadata := map[string]string{
			"parser.source_format": "xlsx", "parser.source_part": section.name,
			"parser.source_sha256": source.fileSHA256, "parser.worksheet": section.sheet,
			"parser.worksheet_rows": fmt.Sprint(section.rows), "parser.worksheet_columns": fmt.Sprint(section.columns),
			"parser.worksheet_cells": fmt.Sprint(section.cells),
		}
		parsed.segments = append(parsed.segments, types.ParserDefinedSegment{
			Seq: len(parsed.segments), Start: position, End: position + length,
			ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks, Metadata: metadata,
			ParsedChunks: []types.ParserChunkSpan{{Seq: 0, Start: 0, End: length, Metadata: metadata}},
		})
		position += length
		text.WriteString(section.markdown)
	}
	parsed.markdown = text.String()
	type assetReceipt struct {
		Name, MediaType, SHA256 string
		Bytes                   int
	}
	var assets []assetReceipt
	for _, asset := range parsed.assets {
		digest := sha256.Sum256(asset.Data)
		assets = append(assets, assetReceipt{
			Name: asset.Name, MediaType: asset.MediaType, SHA256: hex.EncodeToString(digest[:]), Bytes: len(asset.Data),
		})
	}
	textSHA := sha256.Sum256([]byte(parsed.markdown))
	canonical, err := json.Marshal(struct {
		FileSHA256, MarkdownSHA256, ParserVersion string
		Assets                                    []assetReceipt
		Segments                                  []types.ParserDefinedSegment
	}{source.fileSHA256, hex.EncodeToString(textSHA[:]), parsed.parserVersion, assets, parsed.segments})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(canonical)
	parsed.outputSHA256 = hex.EncodeToString(digest[:])
	if _, err = p.revalidateExactPersonalDocument(ctx, source); err != nil {
		return nil, err
	}
	return parsed, nil
}
