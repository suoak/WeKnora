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

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/Tencent/WeKnora/internal/types"
)

// Parsed bytes have their own provenance. This does not bless an existing
// Knowledge summary, an old Chunk, a cached answer or arbitrary model messages.
type personalExactParsedDocument struct {
	source        *personalExactDocument
	markdown      string
	assets        []anydoc.Asset
	parserVersion string
	outputSHA256  string
	segments      []types.ParserDefinedSegment
}

func (p *PersonalRecovery) revalidateExactPersonalDocument(
	ctx context.Context, proof *personalExactDocument,
) (*personalExactDocument, error) {
	if proof == nil || proof.attempt == "" || proof.nonce == "" {
		return nil, errPersonalExactDocument
	}
	_, _, current, err := p.repo.Load(ctx, proof.attempt)
	if err != nil {
		return nil, err
	}
	if current.OwnerNonce != proof.nonce || current.Fence != proof.generation ||
		current.CandidateVersionID != proof.candidate || current.ExpectedActiveVersion != proof.expectedActive ||
		current.GrantEpoch != proof.grantEpoch || current.AppConfigVersion != proof.appVersion {
		return nil, errPersonalExactDocument
	}
	fresh, err := p.prepareExactPersonalDocument(ctx, current)
	if err != nil {
		return nil, err
	}
	if fresh.knowledge != proof.knowledge || fresh.kb != proof.kb || fresh.tenant != proof.tenant ||
		fresh.importID != proof.importID || fresh.resource != proof.resource ||
		fresh.resourceHandle != proof.resourceHandle ||
		fresh.fileSHA256 != proof.fileSHA256 || fresh.manifestSHA256 != proof.manifestSHA256 ||
		fresh.fileMD5 != proof.fileMD5 || fresh.physicalPath != proof.physicalPath ||
		fresh.bindingID != proof.bindingID ||
		fresh.connectionIdentity != proof.connectionIdentity || fresh.revision != proof.revision {
		return nil, errPersonalExactDocument
	}
	return fresh, nil
}

// The parser receives the checked immutable buffer, never a task-supplied URL
// or a file path reopened after verification. Model/input and publication
// receipts are still required before any subsequent model call or activation.
func (p *PersonalRecovery) parseExactPersonalDocument(
	ctx context.Context, proof *personalExactDocument,
) (*personalExactParsedDocument, error) {
	fresh, err := p.revalidateExactPersonalDocument(ctx, proof)
	if err != nil {
		return nil, err
	}
	_, _, a, err := p.repo.Load(ctx, fresh.attempt)
	if err != nil {
		return nil, err
	}
	if strings.ToLower(filepath.Ext(a.ManifestName)) != ".docx" {
		return nil, errPersonalExactDocument
	}
	workbooks, err := prepareExactEmbeddedWorkbooks(fresh.file)
	if err != nil {
		return nil, fmt.Errorf("exact_document_dependencies_unverified: %w", err)
	}
	parsed, err := anydoc.Convert(fresh.file, anydoc.Options{Format: "docx", WithAssets: true})
	if err != nil {
		return nil, err
	}
	if parsed == nil || parsed.AssetsError != nil {
		return nil, fmt.Errorf("exact_document_parser_assets_unverified: %w", errPersonalExactDocument)
	}
	if err = replaceExactWorkbookPlaceholders(parsed, workbooks); err != nil {
		return nil, err
	}
	var segments []types.ParserDefinedSegment
	var workbookDigests []string
	if len(workbooks) > 0 {
		end := utf8.RuneCountInString(parsed.Markdown)
		if end == 0 {
			return nil, errPersonalExactDocument
		}
		segments = append(segments, types.ParserDefinedSegment{
			Seq: 0, Start: 0, End: end, ChunkingPolicy: types.ChunkingPolicyDefault,
		})
		var nextAssetID uint64
		for _, asset := range parsed.Assets {
			nextAssetID = max(nextAssetID, asset.ID+1)
		}
		for _, workbook := range workbooks {
			workbookDigests = append(workbookDigests,
				workbook.Name+":"+workbook.SHA256+":"+workbook.NativeMarkdownSHA256)
			for _, asset := range workbook.Assets {
				asset.ID = nextAssetID
				nextAssetID++
				parsed.Assets = append(parsed.Assets, asset)
			}
			for _, section := range workbook.Sections {
				length := utf8.RuneCountInString(section.markdown)
				metadata := map[string]string{
					"parser.source_format": "embedded_xlsx", "parser.source_part": workbook.Name,
					"parser.source_sha256": workbook.SHA256, "parser.worksheet": section.sheet,
					"parser.worksheet_rows": fmt.Sprint(
						section.rows,
					), "parser.worksheet_columns": fmt.Sprint(section.columns),
					"parser.worksheet_cells": fmt.Sprint(section.cells),
				}
				segments = append(segments, types.ParserDefinedSegment{
					Seq: len(segments), Start: end, End: end + length,
					ChunkingPolicy: types.ChunkingPolicyPreserveParserChunks,
					Metadata:       metadata, ParsedChunks: []types.ParserChunkSpan{{
						Seq: 0, Start: 0, End: length, Metadata: metadata,
					}},
				})
				parsed.Markdown += section.markdown
				end += length
			}
		}
	}
	if _, err = p.revalidateExactPersonalDocument(ctx, fresh); err != nil {
		return nil, err
	}
	type assetReceipt struct {
		ID        uint64 `json:"id"`
		MediaType string `json:"media_type"`
		Name      string `json:"name"`
		SHA256    string `json:"sha256"`
		Bytes     int    `json:"bytes"`
	}
	assets := make([]anydoc.Asset, len(parsed.Assets))
	var receipts []assetReceipt
	for i, asset := range parsed.Assets {
		assets[i] = asset
		assets[i].Data = append([]byte(nil), asset.Data...)
		h := sha256.Sum256(asset.Data)
		receipts = append(
			receipts,
			assetReceipt{
				ID: asset.ID, Name: asset.Name, MediaType: asset.MediaType,
				SHA256: hex.EncodeToString(h[:]), Bytes: len(asset.Data),
			},
		)
	}
	textSHA := sha256.Sum256([]byte(parsed.Markdown))
	canonical, err := json.Marshal(struct {
		FileSHA256, MarkdownSHA256, ParserVersion string
		Assets                                    []assetReceipt
	}{fresh.fileSHA256, hex.EncodeToString(textSHA[:]), anydoc.Version(), receipts})
	if err != nil {
		return nil, err
	}
	if len(workbooks) > 0 {
		canonical, err = json.Marshal(struct {
			Parent    json.RawMessage
			Workbooks []string
			Segments  []types.ParserDefinedSegment
		}{canonical, workbookDigests, segments})
		if err != nil {
			return nil, err
		}
	}
	digest := sha256.Sum256(canonical)
	return &personalExactParsedDocument{
		source: fresh, markdown: parsed.Markdown, assets: assets,
		parserVersion: anydoc.Version(), outputSHA256: hex.EncodeToString(digest[:]),
		segments: segments,
	}, nil
}
