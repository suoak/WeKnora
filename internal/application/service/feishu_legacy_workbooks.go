package service

import (
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/Tencent/WeKnora/internal/types"
)

// expandLegacyWorkbooks handles parser-issued embedded packages, never remote
// URLs. The closed workbook reader proves complete cell coverage before an OLE
// image placeholder can be replaced. Unknown packages remain refused.
func expandLegacyWorkbooks(parsed *types.ReadResult, fileType string) error {
	if parsed == nil || fileType != "docx" {
		return nil
	}
	if err := checkLegacyWorkbookParentBudget(parsed); err != nil {
		return err
	}
	var workbooks []exactEmbeddedWorkbook
	converted := &anydoc.Result{Markdown: parsed.MarkdownContent}
	for _, ref := range parsed.ImageRefs {
		if ref.MimeType != "application/vnd.ms-ole-object" {
			continue
		}
		workbook, err := parseExactEmbeddedWorkbook(
			fmt.Sprintf("embedded-workbook-%d.xlsx", len(workbooks)+1), ref.ImageData, len(workbooks)+1,
		)
		if err != nil {
			return err
		}
		workbooks = append(workbooks, *workbook)
		converted.Assets = append(converted.Assets, anydoc.Asset{
			Name: ref.Filename, MediaType: ref.MimeType, Data: ref.ImageData,
		})
	}
	if len(workbooks) == 0 {
		return nil
	}
	if err := replaceExactWorkbookPlaceholders(converted, workbooks); err != nil {
		return err
	}
	refs := make([]types.ImageRef, 0, len(parsed.ImageRefs))
	used := map[string]bool{}
	for _, ref := range parsed.ImageRefs {
		if ref.MimeType != "application/vnd.ms-ole-object" {
			refs = append(refs, ref)
			used[ref.OriginalRef] = true
		}
	}
	for _, workbook := range workbooks {
		for _, section := range workbook.Sections {
			converted.Markdown += section.markdown
		}
		for _, asset := range workbook.Assets {
			ref := anydoc.ImageDir + asset.Name
			if used[ref] {
				return errExactWorkbookUnsupported
			}
			used[ref] = true
			refs = append(refs, types.ImageRef{
				Filename: asset.Name, OriginalRef: ref, MimeType: asset.MediaType, ImageData: asset.Data,
			})
		}
	}
	parsed.MarkdownContent, parsed.ImageRefs = converted.Markdown, refs
	return nil
}

// Apply the same whole-parent bounds as personal exact imports BEFORE native
// conversion. Count alone rejected small legitimate parents with five packages,
// while four packages could previously multiply the per-workbook expansion cap.
func checkLegacyWorkbookParentBudget(parsed *types.ReadResult) error {
	packages := map[string][]byte{}
	var compressed uint64
	for _, ref := range parsed.ImageRefs {
		if ref.MimeType != "application/vnd.ms-ole-object" {
			continue
		}
		if len(packages) >= 64 || len(parsed.ParsedChunks) != 0 || len(parsed.ParsedSegments) != 0 ||
			len(parsed.SourceBlocks) != 0 || !strings.HasPrefix(ref.OriginalRef, anydoc.ImageDir) ||
			ref.OriginalRef != anydoc.ImageDir+ref.Filename || ref.Filename == "" ||
			packages[ref.Filename] != nil || len(ref.ImageData) == 0 || len(ref.ImageData) > 8<<20 {
			return errExactWorkbookUnsupported
		}
		compressed += uint64(len(ref.ImageData))
		if compressed > 32<<20 {
			return errExactWorkbookUnsupported
		}
		packages[ref.Filename] = ref.ImageData
	}
	return checkExactWorkbookExpansionBudget(packages)
}
