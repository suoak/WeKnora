package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/xuri/excelize/v2"
)

var errExactWorkbookUnsupported = errors.New("exact_embedded_xlsx_dependency_unsupported")

type exactWorkbookSection struct {
	name, sheet, markdown string
	rows, columns, cells  int
}

type exactEmbeddedWorkbook struct {
	Name, SHA256, NativeMarkdownSHA256 string
	Sections                           []exactWorkbookSection
	Assets                             []anydoc.Asset
}

func prepareExactEmbeddedWorkbooks(data []byte) ([]exactEmbeddedWorkbook, error) {
	if !exactEmbeddedWorkbooksEnabled() {
		return nil, checkExactDOCXDependencies(data)
	}
	packages := map[string][]byte{}
	if err := checkExactDOCXGraph(data, packages); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(packages))
	for name := range packages {
		names = append(names, name)
	}
	sort.Strings(names)
	workbooks := make([]exactEmbeddedWorkbook, 0, len(names))
	for index, name := range names {
		workbook, err := parseExactEmbeddedWorkbook(name, packages[name], index+1)
		if err != nil {
			return nil, err
		}
		workbooks = append(workbooks, *workbook)
	}
	return workbooks, nil
}

func exactEmbeddedWorkbooksEnabled() bool {
	return os.Getenv("FEISHU_PERSONAL_EXACT_EMBEDDED_XLSX") == "1"
}

// This is a closed, bounded Office package reader. It never extracts a path,
// resolves an external relationship, executes a formula, or authorizes bytes
// outside the already authenticated parent DOCX.
func exactWorkbookParts(data []byte) (map[string][]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(z.File) == 0 || len(z.File) > 256 {
		return nil, errExactWorkbookUnsupported
	}
	parts := map[string][]byte{}
	var size uint64
	for _, entry := range z.File {
		name := strings.ToLower(entry.Name)
		allowed := name == "[content_types].xml" || name == "_rels/.rels" ||
			name == "docprops/core.xml" || name == "docprops/app.xml" ||
			name == "xl/workbook.xml" || name == "xl/_rels/workbook.xml.rels" ||
			name == "xl/sharedstrings.xml" || name == "xl/styles.xml" || name == "xl/metadata.xml" ||
			strings.HasPrefix(name, "xl/theme/") && strings.HasSuffix(name, ".xml") ||
			strings.HasPrefix(name, "xl/worksheets/") &&
				(strings.HasSuffix(name, ".xml") || strings.HasSuffix(name, ".rels")) ||
			strings.HasPrefix(name, "xl/media/") && strings.HasSuffix(name, ".png")
		if !allowed || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") ||
			path.Clean(name) != name || parts[name] != nil || entry.UncompressedSize64 > 8<<20 ||
			size > (32<<20)-entry.UncompressedSize64 {
			return nil, errExactWorkbookUnsupported
		}
		size += entry.UncompressedSize64
		r, openErr := entry.Open()
		if openErr != nil {
			return nil, errExactWorkbookUnsupported
		}
		body, readErr := io.ReadAll(io.LimitReader(r, (8<<20)+1))
		_ = r.Close()
		if readErr != nil || uint64(len(body)) != entry.UncompressedSize64 {
			return nil, errExactWorkbookUnsupported
		}
		parts[name] = body
	}
	for name, body := range parts {
		if !strings.HasSuffix(name, ".xml") && !strings.HasSuffix(name, ".rels") {
			continue
		}
		decoder := xml.NewDecoder(bytes.NewReader(body))
		ids := map[string]bool{}
		for {
			token, tokenErr := decoder.Token()
			if tokenErr == io.EOF {
				break
			}
			if tokenErr != nil {
				return nil, errExactWorkbookUnsupported
			}
			if _, directive := token.(xml.Directive); directive {
				return nil, errExactWorkbookUnsupported
			}
			start, ok := token.(xml.StartElement)
			if !ok {
				continue
			}
			// These dependencies carry data that this bounded reader cannot
			// completely represent. Preserve pending instead of omitting it.
			switch start.Name.Local {
			case "f", "formula", "mergeCell", "hyperlink", "drawing", "legacyDrawing",
				"oleObject", "control", "externalReference", "AlternateContent":
				return nil, errExactWorkbookUnsupported
			}
			if start.Name.Local != "Relationship" || !strings.HasSuffix(name, ".rels") {
				continue
			}
			a := map[string]string{}
			for _, attr := range start.Attr {
				a[attr.Name.Local] = attr.Value
			}
			target := a["Target"]
			if a["Id"] == "" || ids[a["Id"]] || a["TargetMode"] != "" && a["TargetMode"] != "Internal" ||
				target == "" || strings.ContainsAny(target, "\\:%?#\x00") || strings.HasPrefix(target, "//") ||
				strings.HasSuffix(a["Type"], "/package") || strings.HasSuffix(a["Type"], "/oleObject") {
				return nil, errExactWorkbookUnsupported
			}
			ids[a["Id"]] = true
			base := path.Dir(path.Dir(name))
			if strings.HasPrefix(target, "/") {
				base = "."
			}
			resolved := strings.ToLower(path.Clean(path.Join(base, strings.TrimPrefix(target, "/"))))
			if strings.HasPrefix(resolved, "../") || parts[resolved] == nil {
				return nil, errExactWorkbookUnsupported
			}
		}
	}
	return parts, nil
}

func exactWorkbookCellGrid(file *excelize.File, sheet string, body []byte) (string, int, int, int, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	cells := map[string]bool{}
	maxRow, maxCol := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", 0, 0, 0, errExactWorkbookUnsupported
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "c" {
			continue
		}
		ref, kind := "", ""
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "r":
				ref = attr.Value
			case "t":
				kind = attr.Value
			case "s":
			default:
				return "", 0, 0, 0, errExactWorkbookUnsupported
			}
		}
		col, row, err := excelize.CellNameToCoordinates(ref)
		if err != nil || row > 1024 || col > 64 || cells[ref] || len(cells) >= 10000 ||
			kind != "" && kind != "s" && kind != "inlineStr" && kind != "n" && kind != "b" {
			return "", 0, 0, 0, errExactWorkbookUnsupported
		}
		cells[ref] = true
		maxRow, maxCol = max(maxRow, row), max(maxCol, col)
	}
	if len(cells) == 0 {
		return "", 0, 0, 0, errExactWorkbookUnsupported
	}
	escape := func(value string) string {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;",
			"|", "&#124;", "\r", "", "\n", "<br>").Replace(value)
	}
	var grid strings.Builder
	grid.WriteString("| Row |")
	for column := 1; column <= maxCol; column++ {
		name, _ := excelize.ColumnNumberToName(column)
		grid.WriteString(" " + name + " |")
	}
	grid.WriteString("\n| --- |")
	for column := 1; column <= maxCol; column++ {
		grid.WriteString(" --- |")
	}
	grid.WriteString("\n")
	for row := 1; row <= maxRow; row++ {
		grid.WriteString("| " + strconv.Itoa(row) + " |")
		for column := 1; column <= maxCol; column++ {
			ref, _ := excelize.CoordinatesToCellName(column, row)
			value, err := file.GetCellValue(sheet, ref, excelize.Options{RawCellValue: true})
			if err != nil {
				return "", 0, 0, 0, errExactWorkbookUnsupported
			}
			grid.WriteString(" " + escape(value) + " |")
		}
		grid.WriteString("\n")
		// Keep the entire worksheet together. Never split a row or silently
		// truncate a workbook to fit a model/chunk limit.
		if utf8.RuneCountInString(grid.String()) > types.DefaultParserSemanticChunkMaxChars-1024 {
			return "", 0, 0, 0, errExactWorkbookUnsupported
		}
	}
	return grid.String(), maxRow, maxCol, len(cells), nil
}

func parseExactEmbeddedWorkbook(name string, data []byte, ordinal int) (*exactEmbeddedWorkbook, error) {
	parts, err := exactWorkbookParts(data)
	if err != nil {
		return nil, err
	}
	file, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{
		UnzipSizeLimit: 32 << 20, UnzipXMLSizeLimit: 8 << 20,
	})
	if err != nil {
		return nil, errExactWorkbookUnsupported
	}
	defer file.Close()
	// The six approved fixtures have one worksheet. Reject other layouts
	// until worksheet relationship mapping and complete coverage are proved.
	sheets := file.GetSheetList()
	var worksheetParts []string
	for part := range parts {
		if strings.HasPrefix(part, "xl/worksheets/") && !strings.Contains(part, "/_rels/") &&
			strings.HasSuffix(part, ".xml") {
			worksheetParts = append(worksheetParts, part)
		}
	}
	if len(sheets) != 1 || len(worksheetParts) != 1 {
		return nil, errExactWorkbookUnsupported
	}
	grid, rows, columns, cells, err := exactWorkbookCellGrid(file, sheets[0], parts[worksheetParts[0]])
	if err != nil {
		return nil, err
	}
	native, err := anydoc.Convert(data, anydoc.Options{Format: "xlsx", WithAssets: true})
	if err != nil || native == nil || native.AssetsError != nil || strings.TrimSpace(native.Markdown) == "" {
		return nil, errExactWorkbookUnsupported
	}
	digest := func(data []byte) string {
		h := sha256.Sum256(data)
		return hex.EncodeToString(h[:])
	}
	result := &exactEmbeddedWorkbook{
		Name: name, SHA256: digest(data), NativeMarkdownSHA256: digest([]byte(native.Markdown)),
	}
	header := strings.NewReplacer("\n", " ", "\r", " ", "<", "&lt;", ">", "&gt;")
	markdown := "\n\n## Embedded workbook: " + header.Replace(path.Base(name)) +
		"\n\n### Worksheet: " + header.Replace(sheets[0]) + "\n\n" + grid
	var images []string
	for part := range parts {
		if strings.HasPrefix(part, "xl/media/") {
			images = append(images, part)
		}
	}
	sort.Strings(images)
	for index, part := range images {
		image := parts[part]
		config, imageErr := png.DecodeConfig(bytes.NewReader(image))
		if imageErr != nil || config.Width <= 0 || config.Height <= 0 ||
			int64(config.Width)*int64(config.Height) > 16<<20 {
			return nil, errExactWorkbookUnsupported
		}
		if _, imageErr = png.Decode(bytes.NewReader(image)); imageErr != nil {
			return nil, errExactWorkbookUnsupported
		}
		imageName := fmt.Sprintf("embedded-workbook-%d-image-%d.png", ordinal, index+1)
		result.Assets = append(result.Assets, anydoc.Asset{
			Name: imageName, MediaType: "image/png", Data: append([]byte(nil), image...),
			Alt: "Embedded workbook image", Section: sheets[0],
		})
		markdown += "\n\n![Embedded workbook image](" + anydoc.ImageDir + imageName + ")\n"
	}
	if utf8.RuneCountInString(markdown) > types.DefaultParserSemanticChunkMaxChars {
		return nil, errExactWorkbookUnsupported
	}
	result.Sections = []exactWorkbookSection{{
		name: name, sheet: sheets[0], markdown: markdown, rows: rows, columns: columns, cells: cells,
	}}
	return result, nil
}
