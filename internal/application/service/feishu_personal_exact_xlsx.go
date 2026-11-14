package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"image"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/xuri/excelize/v2"
)

const exactStandaloneXLSXVersion = "exact-xlsx-v1"

type standaloneWorkbookSheet struct {
	Name  string `xml:"name,attr"`
	ID    int    `xml:"sheetId,attr"`
	RID   string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	State string `xml:"state,attr"`
	Part  string
}

type standaloneWorkbookRelation struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
	Mode   string `xml:"TargetMode,attr"`
}

func standaloneWorkbookRelations(parts map[string][]byte, owner string) ([]standaloneWorkbookRelation, error) {
	name := path.Dir(owner) + "/_rels/" + path.Base(owner) + ".rels"
	if owner == "" {
		name = "_rels/.rels"
	}
	body := parts[name]
	if len(body) == 0 {
		return nil, nil
	}
	var document struct {
		XMLName xml.Name `xml:"http://schemas.openxmlformats.org/package/2006/relationships Relationships"`

		Rows []standaloneWorkbookRelation `xml:"Relationship"`
	}
	if xml.Unmarshal(body, &document) != nil {
		return nil, errExactWorkbookUnsupported
	}
	return document.Rows, nil
}

func standaloneRelationshipTarget(owner string, rel standaloneWorkbookRelation) string {
	if strings.HasPrefix(rel.Target, "/") {
		return strings.ToLower(strings.TrimPrefix(rel.Target, "/"))
	}
	return strings.ToLower(path.Clean(path.Join(path.Dir(owner), rel.Target)))
}

func standaloneWorkbookSheets(
	parts map[string][]byte, file *excelize.File,
) ([]standaloneWorkbookSheet, error) {
	var workbook struct {
		XMLName xml.Name                  `xml:"http://schemas.openxmlformats.org/spreadsheetml/2006/main workbook"`
		Sheets  []standaloneWorkbookSheet `xml:"sheets>sheet"`
	}
	if xml.Unmarshal(parts["xl/workbook.xml"], &workbook) != nil ||
		len(workbook.Sheets) == 0 || len(workbook.Sheets) > 64 {
		return nil, errExactWorkbookUnsupported
	}
	rels, err := standaloneWorkbookRelations(parts, "xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	byID := map[string]string{}
	for _, rel := range rels {
		if rel.Type == exactWorkbookRelNamespace+"/worksheet" {
			byID[rel.ID] = standaloneRelationshipTarget("xl/workbook.xml", rel)
		}
	}
	names := file.GetSheetList()
	if len(names) != len(workbook.Sheets) || len(byID) != len(names) {
		return nil, errExactWorkbookUnsupported
	}
	seenParts, seenNames, seenIDs := map[string]bool{}, map[string]bool{}, map[int]bool{}
	for i := range workbook.Sheets {
		sheet := &workbook.Sheets[i]
		sheet.Part = byID[sheet.RID]
		if sheet.ID <= 0 || seenIDs[sheet.ID] || sheet.Name == "" || seenNames[sheet.Name] ||
			sheet.Name != names[i] || seenParts[sheet.Part] || parts[sheet.Part] == nil ||
			path.Dir(sheet.Part) != "xl/worksheets" || !strings.HasSuffix(sheet.Part, ".xml") ||
			(sheet.State != "" && sheet.State != "visible" && sheet.State != "hidden" && sheet.State != "veryHidden") {
			return nil, errExactWorkbookUnsupported
		}
		seenParts[sheet.Part], seenNames[sheet.Name], seenIDs[sheet.ID] = true, true, true
	}
	for name := range parts {
		if path.Dir(name) == "xl/worksheets" && !seenParts[name] {
			return nil, errExactWorkbookUnsupported
		}
	}
	return workbook.Sheets, nil
}

func standaloneArrayRange(ref, anchor string) bool {
	ends := strings.Split(ref, ":")
	if len(ends) == 1 {
		ends = append(ends, ends[0])
	}
	if len(ends) != 2 || ends[0] != anchor {
		return false
	}
	x1, y1, err := excelize.CellNameToCoordinates(ends[0])
	x2, y2, err2 := excelize.CellNameToCoordinates(ends[1])
	return err == nil && err2 == nil && x1 > 0 && y1 > 0 && x1 <= x2 && y1 <= y2 && x2 <= 64 && y2 <= 1024
}

func parseExactStandaloneWorkbook(ctx context.Context, name string, data []byte) (*exactEmbeddedWorkbook, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parts, err := exactWorkbookPartsForFormat(data, true)
	if err != nil {
		return nil, fmt.Errorf("standalone_xlsx_package: %w", err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{
		UnzipSizeLimit: 32 << 20, UnzipXMLSizeLimit: 8 << 20,
	})
	if err != nil {
		return nil, fmt.Errorf("standalone_xlsx_open: %w", errExactWorkbookUnsupported)
	}
	defer func() { _ = file.Close() }()
	sheets, err := standaloneWorkbookSheets(parts, file)
	if err != nil {
		return nil, fmt.Errorf("standalone_xlsx_sheets: %w", err)
	}
	if err = validateStandaloneWorkbookGraph(parts); err != nil {
		return nil, fmt.Errorf("standalone_xlsx_graph: %w", err)
	}
	extras, err := standaloneWorkbookFacts(parts, sheets, file)
	if err != nil {
		return nil, fmt.Errorf("standalone_xlsx_metadata: %w", err)
	}
	links, err := exactWorkbookHyperlinkNotesForFormat(parts, true)
	if err != nil {
		return nil, fmt.Errorf("standalone_xlsx_links: %w", err)
	}
	drawings, err := exactWorkbookDrawingNotes(parts)
	if err != nil {
		return nil, fmt.Errorf("standalone_xlsx_drawings: %w", err)
	}
	comments, err := exactWorkbookCommentNotes(parts)
	if err != nil {
		return nil, fmt.Errorf("standalone_xlsx_comments: %w", err)
	}
	sum := sha256.Sum256(data)
	result := &exactEmbeddedWorkbook{Name: name, SHA256: hex.EncodeToString(sum[:])}
	imageNames, err := standaloneWorkbookAssets(parts, result)
	if err != nil {
		return nil, err
	}
	header := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", " ", "\n", " ")
	total := 0
	for _, sheet := range sheets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		grid, rows, cols, cells, err := exactWorkbookCellGridForFormat(file, sheet.Name, parts[sheet.Part], true)
		if err != nil {
			return nil, fmt.Errorf("standalone_xlsx_cells: %w", err)
		}
		formulas, err := exactWorkbookFormulaNotesForFormat(file, sheet.Name, parts[sheet.Part], true)
		if err != nil {
			return nil, fmt.Errorf("standalone_xlsx_formulas: %w", err)
		}
		grid += links[sheet.Part] + formulas + drawings[sheet.Part] + comments[sheet.Part] + extras[sheet.Part]
		assets, err := standaloneSheetImages(parts, sheet.Part)
		if err != nil {
			return nil, err
		}
		for _, imagePart := range assets {
			imageName := imageNames[imagePart]
			if imageName == "" {
				return nil, errExactWorkbookUnsupported
			}
			grid += "\n![Worksheet image](" + anydoc.ImageDir + imageName + ")\n"
		}
		heading := "\n\n## Workbook: " + header.Replace(path.Base(name)) +
			"\n\n### Worksheet: " + header.Replace(sheet.Name) + " (state=" + sheet.State + ")\n\n"
		sections, err := splitExactWorkbookRows(heading+grid, heading)
		if err != nil {
			return nil, err
		}
		for _, text := range sections {
			total += len(text)
			if total > 8<<20 || len(result.Sections) >= 1024 {
				return nil, errExactWorkbookUnsupported
			}
			result.Sections = append(result.Sections, exactWorkbookSection{
				name: sheet.Part, sheet: sheet.Name, markdown: text, rows: rows, columns: cols, cells: cells,
			})
		}
	}
	return result, nil
}

func standaloneWorkbookAssets(parts map[string][]byte, result *exactEmbeddedWorkbook) (map[string]string, error) {
	var names []string
	for part := range parts {
		if strings.HasPrefix(part, "xl/media/") {
			names = append(names, part)
		}
	}
	sort.Strings(names)
	if len(names) > 64 {
		return nil, errExactWorkbookUnsupported
	}
	byPart := map[string]string{}
	for index, part := range names {
		body := parts[part]
		config, format, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16<<20 {
			return nil, errExactWorkbookUnsupported
		}
		if _, _, err = image.Decode(bytes.NewReader(body)); err != nil || (format != "png" && format != "jpeg") {
			return nil, errExactWorkbookUnsupported
		}
		ext, media := ".png", "image/png"
		if format == "jpeg" {
			ext, media = ".jpg", "image/jpeg"
		}
		name := "standalone-workbook-image-" + strconv.Itoa(index+1) + ext
		byPart[part] = name
		result.Assets = append(result.Assets, anydoc.Asset{
			ID: uint64(index), Name: name, MediaType: media, Data: append([]byte(nil), body...), Alt: "Worksheet image",
		})
	}
	return byPart, nil
}

func standaloneSheetImages(parts map[string][]byte, sheet string) ([]string, error) {
	if err := standaloneImageReferences(parts, sheet, "picture", "drawing"); err != nil {
		return nil, err
	}
	rels, err := standaloneWorkbookRelations(parts, sheet)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, rel := range rels {
		switch rel.Type {
		case exactWorkbookRelNamespace + "/image":
			seen[standaloneRelationshipTarget(sheet, rel)] = true
		case exactWorkbookRelNamespace + "/drawing":
			drawing := standaloneRelationshipTarget(sheet, rel)
			if err := standaloneImageReferences(parts, drawing, "blip", ""); err != nil {
				return nil, err
			}
			images, err := standaloneWorkbookRelations(parts, drawing)
			if err != nil {
				return nil, err
			}
			for _, image := range images {
				if image.Type != exactWorkbookRelNamespace+"/image" {
					return nil, errExactWorkbookUnsupported
				}
				seen[standaloneRelationshipTarget(drawing, image)] = true
			}
		}
	}
	var result []string
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}
