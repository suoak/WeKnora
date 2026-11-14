package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
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

func replaceExactWorkbookPlaceholders(parsed *anydoc.Result, workbooks []exactEmbeddedWorkbook) error {
	if len(workbooks) == 0 {
		return nil
	}
	covered := map[string]bool{}
	kept := make([]anydoc.Asset, 0, len(parsed.Assets))
	for _, asset := range parsed.Assets {
		if asset.MediaType != "application/vnd.ms-ole-object" {
			kept = append(kept, asset)
			continue
		}
		sum := sha256.Sum256(asset.Data)
		digest := hex.EncodeToString(sum[:])
		matched := false
		for _, workbook := range workbooks {
			if digest != workbook.SHA256 {
				continue
			}
			if covered[workbook.Name] {
				return errExactWorkbookUnsupported
			}
			markdown, count := docparser.ReplacePackageImageReference(
				parsed.Markdown, anydoc.ImageDir+asset.Name,
			)
			if count != 1 {
				return errExactWorkbookUnsupported
			}
			parsed.Markdown = markdown
			covered[workbook.Name], matched = true, true
			break
		}
		if !matched {
			return errExactWorkbookUnsupported
		}
	}
	if len(covered) != len(workbooks) {
		return errExactWorkbookUnsupported
	}
	parsed.Assets = kept
	return nil
}

func prepareExactEmbeddedWorkbooks(data []byte) ([]exactEmbeddedWorkbook, error) {
	if !exactEmbeddedWorkbooksEnabled() {
		return nil, checkExactDOCXDependencies(data)
	}
	packages := map[string][]byte{}
	if err := checkExactDOCXGraph(data, packages); err != nil {
		return nil, err
	}
	if err := checkExactWorkbookExpansionBudget(packages); err != nil {
		return nil, err
	}
	if len(packages) > 0 {
		if err := verifyExactWorkbookObjects(data, packages); err != nil {
			return nil, err
		}
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

func verifyExactWorkbookObjects(data []byte, packages map[string][]byte) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return errExactWorkbookUnsupported
	}
	parts := map[string][]byte{}
	for _, entry := range archive.File {
		if entry.Name != "word/document.xml" && entry.Name != "word/_rels/document.xml.rels" {
			continue
		}
		reader, openErr := entry.Open()
		if openErr != nil {
			return errExactWorkbookUnsupported
		}
		body, readErr := io.ReadAll(io.LimitReader(reader, (8<<20)+1))
		_ = reader.Close()
		if readErr != nil || len(body) > 8<<20 {
			return errExactWorkbookUnsupported
		}
		parts[entry.Name] = body
	}
	type relationship struct {
		ID, Target, Type string
	}
	references := map[string]string{}
	for name, body := range parts {
		decoder := xml.NewDecoder(bytes.NewReader(body))
		for {
			token, tokenErr := decoder.Token()
			if tokenErr == io.EOF {
				break
			}
			if tokenErr != nil {
				return errExactWorkbookUnsupported
			}
			start, ok := token.(xml.StartElement)
			if !ok || name != "word/_rels/document.xml.rels" || start.Name.Local != "Relationship" {
				continue
			}
			var ref relationship
			for _, attr := range start.Attr {
				switch attr.Name.Local {
				case "Id":
					ref.ID = attr.Value
				case "Target":
					ref.Target = attr.Value
				case "Type":
					ref.Type = attr.Value
				}
			}
			if strings.HasSuffix(ref.Type, "/package") {
				target := strings.ToLower(path.Clean(path.Join("word", ref.Target)))
				if ref.ID == "" || references[ref.ID] != "" || packages[target] == nil {
					return errExactWorkbookUnsupported
				}
				references[ref.ID] = target
			}
		}
	}
	decoder := xml.NewDecoder(bytes.NewReader(parts["word/document.xml"]))
	objects := map[string]int{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errExactWorkbookUnsupported
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "OLEObject" {
			continue
		}
		progID, kind, id := "", "", ""
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "ProgID":
				progID = attr.Value
			case "Type":
				kind = attr.Value
			case "id":
				if attr.Name.Space == "http://schemas.openxmlformats.org/officeDocument/2006/relationships" {
					id = attr.Value
				}
			}
		}
		if progID != "Excel.Sheet.12" || kind != "Embed" || references[id] == "" {
			return errExactWorkbookUnsupported
		}
		objects[references[id]]++
	}
	for name := range packages {
		if objects[name] != 1 {
			return errExactWorkbookUnsupported
		}
	}
	return nil
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
			strings.HasPrefix(name, "xl/drawings/") &&
				(strings.HasSuffix(name, ".xml") || strings.HasSuffix(name, ".rels")) ||
			strings.HasPrefix(name, "xl/media/") && (strings.HasSuffix(name, ".png") ||
				strings.HasSuffix(name, ".jpeg") || strings.HasSuffix(name, ".jpg"))
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
			case "formula", "legacyDrawing",
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
			if a["TargetMode"] == "External" && a["Type"] == exactWorkbookHyperlinkType {
				u, parseErr := url.Parse(target)
				if !strings.HasPrefix(name, "xl/worksheets/_rels/") || a["Id"] == "" || ids[a["Id"]] ||
					parseErr != nil || u.User != nil || u.Hostname() == "" ||
					(u.Scheme != "https" && u.Scheme != "http") || strings.ContainsAny(target, "\x00\r\n") {
					return nil, errExactWorkbookUnsupported
				}
				ids[a["Id"]] = true
				continue
			}
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
	if _, err := exactWorkbookHyperlinkNotes(parts); err != nil {
		return nil, err
	}
	if _, err := exactWorkbookDrawingNotes(parts); err != nil {
		return nil, err
	}
	return parts, nil
}

func exactWorkbookCellGrid(file *excelize.File, sheet string, body []byte) (string, int, int, int, error) {
	merged, mergeNote, mergeRow, mergeCol, err := exactWorkbookMerges(body)
	if err != nil {
		return "", 0, 0, 0, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	cells := map[string]bool{}
	maxRow, maxCol := mergeRow, mergeCol
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
		canonical, canonicalErr := excelize.CoordinatesToCellName(col, row)
		if err != nil || row > 1024 || col > 64 || cells[ref] || len(cells) >= 10000 ||
			canonicalErr != nil || canonical != ref ||
			kind != "" && kind != "s" && kind != "inlineStr" &&
				kind != "str" && kind != "n" && kind != "b" {
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
			value := ""
			var err error
			if !merged[ref] {
				value, err = file.GetCellValue(sheet, ref, excelize.Options{RawCellValue: true})
			}
			if err != nil {
				return "", 0, 0, 0, errExactWorkbookUnsupported
			}
			grid.WriteString(" " + escape(value) + " |")
		}
		grid.WriteString("\n")
		// Bound the complete worksheet independently of semantic chunk size.
		// The section builder splits only between complete rows below.
		if grid.Len() > 4<<20 {
			return "", 0, 0, 0, errExactWorkbookUnsupported
		}
	}
	result := grid.String() + mergeNote
	if len(result) > 4<<20 {
		return "", 0, 0, 0, errExactWorkbookUnsupported
	}
	return result, maxRow, maxCol, len(cells), nil
}

// Merged ranges are layout within the authenticated worksheet, not an external
// dependency. Preserve their coordinates and anchor value without duplicating
// that value into covered cells. Reject hidden conflicting values and overlap.
func exactWorkbookMerges(body []byte) (map[string]bool, string, int, int, error) {
	var worksheet struct {
		Rows []struct {
			Cells []struct {
				Ref    string    `xml:"r,attr"`
				Value  string    `xml:"v"`
				Inline *struct{} `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
		Merges []struct {
			Ref string `xml:"ref,attr"`
		} `xml:"mergeCells>mergeCell"`
	}
	if xml.Unmarshal(body, &worksheet) != nil || len(worksheet.Merges) > 1024 {
		return nil, "", 0, 0, errExactWorkbookUnsupported
	}
	covered, occupied := map[string]bool{}, map[string]bool{}
	maxRow, maxCol := 0, 0
	var note strings.Builder
	for _, merge := range worksheet.Merges {
		ends := strings.Split(merge.Ref, ":")
		if len(ends) != 2 {
			return nil, "", 0, 0, errExactWorkbookUnsupported
		}
		left, top, err := excelize.CellNameToCoordinates(ends[0])
		right, bottom, endErr := excelize.CellNameToCoordinates(ends[1])
		if err != nil || endErr != nil || left > right || top > bottom ||
			right > 64 || bottom > 1024 || (left == right && top == bottom) {
			return nil, "", 0, 0, errExactWorkbookUnsupported
		}
		for row := top; row <= bottom; row++ {
			for col := left; col <= right; col++ {
				ref, _ := excelize.CoordinatesToCellName(col, row)
				if occupied[ref] {
					return nil, "", 0, 0, errExactWorkbookUnsupported
				}
				occupied[ref] = true
				covered[ref] = col != left || row != top
			}
		}
		maxRow, maxCol = max(maxRow, bottom), max(maxCol, right)
		anchor, _ := excelize.CoordinatesToCellName(left, top)
		end, _ := excelize.CoordinatesToCellName(right, bottom)
		note.WriteString("\nMerged cells: " + anchor + ":" + end + " (anchor " + anchor + ").\n")
	}
	for _, row := range worksheet.Rows {
		for _, cell := range row.Cells {
			col, row, err := excelize.CellNameToCoordinates(cell.Ref)
			canonical, canonicalErr := excelize.CoordinatesToCellName(col, row)
			if err != nil || canonicalErr != nil || canonical != cell.Ref {
				return nil, "", 0, 0, errExactWorkbookUnsupported
			}
			if covered[cell.Ref] && (cell.Value != "" || cell.Inline != nil) {
				return nil, "", 0, 0, errExactWorkbookUnsupported
			}
		}
	}
	return covered, note.String(), maxRow, maxCol, nil
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
	defer func() { _ = file.Close() }()
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
	notes, err := exactWorkbookHyperlinkNotes(parts)
	if err != nil {
		return nil, err
	}
	grid += notes[worksheetParts[0]]
	formulas, err := exactWorkbookFormulaNotes(file, sheets[0], parts[worksheetParts[0]])
	if err != nil {
		return nil, err
	}
	grid += formulas
	drawings, err := exactWorkbookDrawingNotes(parts)
	if err != nil {
		return nil, err
	}
	grid += drawings[worksheetParts[0]]
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
	heading := "\n\n## Embedded workbook: " + header.Replace(path.Base(name)) +
		"\n\n### Worksheet: " + header.Replace(sheets[0]) + "\n\n"
	markdown := heading + grid
	var images []string
	for part := range parts {
		if strings.HasPrefix(part, "xl/media/") {
			images = append(images, part)
		}
	}
	sort.Strings(images)
	for index, part := range images {
		imageBytes := parts[part]
		config, format, imageErr := image.DecodeConfig(bytes.NewReader(imageBytes))
		if imageErr != nil || config.Width <= 0 || config.Height <= 0 ||
			int64(config.Width)*int64(config.Height) > 16<<20 {
			return nil, errExactWorkbookUnsupported
		}
		if _, _, imageErr = image.Decode(bytes.NewReader(imageBytes)); imageErr != nil ||
			(format != "png" && format != "jpeg") {
			return nil, errExactWorkbookUnsupported
		}
		ext, mediaType := ".png", "image/png"
		if format == "jpeg" {
			ext, mediaType = ".jpg", "image/jpeg"
		}
		imageName := fmt.Sprintf("embedded-workbook-%d-image-%d%s", ordinal, index+1, ext)
		result.Assets = append(result.Assets, anydoc.Asset{
			Name: imageName, MediaType: mediaType, Data: append([]byte(nil), imageBytes...),
			Alt: "Embedded workbook image", Section: sheets[0],
		})
		markdown += "\n\n![Embedded workbook image](" + anydoc.ImageDir + imageName + ")\n"
	}
	sections, err := splitExactWorkbookRows(markdown, heading)
	if err != nil {
		return nil, err
	}
	for _, text := range sections {
		result.Sections = append(result.Sections, exactWorkbookSection{
			name: name, sheet: sheets[0], markdown: text, rows: rows, columns: columns, cells: cells,
		})
	}
	return result, nil
}

// A large worksheet is still one checked source. Repeat its coordinate header
// when splitting at row boundaries so every value keeps its row and column.
// Notes and image references also remain complete; no content is truncated.
func splitExactWorkbookRows(markdown, heading string) ([]string, error) {
	limit := types.DefaultParserSemanticChunkMaxChars
	if utf8.RuneCountInString(markdown) <= limit {
		return []string{markdown}, nil
	}
	lines := strings.SplitAfter(strings.TrimPrefix(markdown, heading), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "| Row |") || !strings.HasPrefix(lines[1], "| --- |") {
		return nil, errExactWorkbookUnsupported
	}
	prefix := heading + lines[0] + lines[1]
	prefixChars := utf8.RuneCountInString(prefix)
	if prefixChars >= limit {
		return nil, errExactWorkbookUnsupported
	}
	current, size := prefix, prefixChars
	var sections []string
	for _, line := range lines[2:] {
		n := utf8.RuneCountInString(line)
		if prefixChars+n > limit {
			return nil, errExactWorkbookUnsupported
		}
		if size+n > limit {
			sections = append(sections, current)
			if len(sections) >= 1024 {
				return nil, errExactWorkbookUnsupported
			}
			current, size = prefix, prefixChars
		}
		current += line
		size += n
	}
	if current != prefix {
		sections = append(sections, current)
	}
	return sections, nil
}
