package feishudocx

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const spreadsheetNS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"

var ErrInvalidXLSXPackage = errors.New("invalid or unsupported Feishu XLSX package")

// CanonicalXLSXDigest reuses the bounded OOXML decoder without changing DOCX's
// identity contract. Repeated authorized Sheet exports vary column widths and
// grouping, despite identical cells, formulas, relationships and binary assets.
// Only width is omitted; column coverage and every other property remain bound.
// All ZIP members are included. Unsupported column structures fail closed.
func CanonicalXLSXDigest(content []byte) (string, string, error) {
	const algorithm = "feishu-xlsx-package-v1"
	if len(content) > maxArchive {
		return "", "", ErrInvalidXLSXPackage
	}
	z, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil || len(z.File) == 0 || len(z.File) > 4096 {
		return "", "", ErrInvalidXLSXPackage
	}
	seen := map[string]bool{}
	var parts []partDigest
	var expanded uint64
	for _, entry := range z.File {
		name := entry.Name
		clean := strings.TrimSuffix(name, "/")
		if clean == "" || !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 ||
			strings.ContainsAny(name, "\\%?#:") || strings.HasPrefix(name, "/") || path.Clean(clean) != clean ||
			clean == ".." || strings.HasPrefix(clean, "../") || seen[strings.ToLower(clean)] {
			return "", "", ErrInvalidXLSXPackage
		}
		seen[strings.ToLower(clean)] = true
		if entry.FileInfo().IsDir() {
			continue
		}
		if !entry.Mode().IsRegular() || entry.UncompressedSize64 > maxPart ||
			entry.CompressedSize64 > uint64(len(content)) ||
			entry.UncompressedSize64 > entry.CompressedSize64*200+1024 {
			return "", "", ErrInvalidXLSXPackage
		}
		expanded += entry.UncompressedSize64
		if expanded > maxExpanded {
			return "", "", ErrInvalidXLSXPackage
		}
		reader, e := entry.Open()
		if e != nil {
			return "", "", ErrInvalidXLSXPackage
		}
		body, readErr := io.ReadAll(io.LimitReader(reader, maxPart+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || len(body) > maxPart || uint64(len(body)) != entry.UncompressedSize64 {
			return "", "", ErrInvalidXLSXPackage
		}
		if strings.HasSuffix(name, ".xml") || strings.HasSuffix(name, ".rels") {
			root, e := parseXML(body)
			if e != nil {
				return "", "", ErrInvalidXLSXPackage
			}
			switch name {
			case "[Content_Types].xml":
				if root.Name != (xml.Name{
					Space: "http://schemas.openxmlformats.org/package/2006/content-types", Local: "Types",
				}) {
					return "", "", ErrInvalidXLSXPackage
				}
			case "_rels/.rels":
				if root.Name != (xml.Name{
					Space: "http://schemas.openxmlformats.org/package/2006/relationships", Local: "Relationships",
				}) {
					return "", "", ErrInvalidXLSXPackage
				}
			case "xl/workbook.xml":
				if root.Name != (xml.Name{Space: spreadsheetNS, Local: "workbook"}) {
					return "", "", ErrInvalidXLSXPackage
				}
			}
			if root.Name == (xml.Name{Space: spreadsheetNS, Local: "worksheet"}) {
				if e := normalizeExportColumnWidths(root); e != nil {
					return "", "", e
				}
			}
			body, _ = json.Marshal(root)
		}
		parts = append(parts, partDigest{Name: name, SHA256: sum(body)})
	}
	for _, required := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml"} {
		if !seen[strings.ToLower(required)] {
			return "", "", ErrInvalidXLSXPackage
		}
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Name < parts[j].Name })
	identity, _ := json.Marshal(struct {
		Algorithm string
		Parts     []partDigest
	}{algorithm, parts})
	return sum(identity), algorithm, nil
}

func normalizeExportColumnWidths(worksheet *node) error {
	blocks := 0
	for _, part := range worksheet.Parts {
		cols := part.Node
		if cols == nil || cols.Name != (xml.Name{Space: spreadsheetNS, Local: "cols"}) {
			continue
		}
		blocks++
		if blocks > 1 {
			return ErrInvalidXLSXPackage
		}
		var result []piece
		last := 0
		for _, p := range cols.Parts {
			if p.Kind == "text" && strings.TrimSpace(p.Value) == "" {
				continue
			}
			col := p.Node
			if col == nil || col.Name != (xml.Name{Space: spreadsheetNS, Local: "col"}) || len(col.Parts) != 0 {
				return ErrInvalidXLSXPackage
			}
			minimum, maximum := 0, 0
			var attrs []xml.Attr
			for _, attr := range col.Attrs {
				if attr.Name.Space != "" {
					attrs = append(attrs, attr)
					continue
				}
				switch attr.Name.Local {
				case "min", "max":
					v, e := strconv.Atoi(attr.Value)
					if e != nil || v < 1 || v > 16384 {
						return ErrInvalidXLSXPackage
					}
					if attr.Name.Local == "min" {
						minimum = v
					} else {
						maximum = v
					}
				case "width":
					v, e := strconv.ParseFloat(attr.Value, 64)
					if e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 255 {
						return ErrInvalidXLSXPackage
					}
				default:
					attrs = append(attrs, attr)
				}
			}
			if minimum <= last || maximum < minimum {
				return ErrInvalidXLSXPackage
			}
			for column := minimum; column <= maximum; column++ {
				n := &node{Name: col.Name, Attrs: append([]xml.Attr(nil), attrs...)}
				n.Attrs = append(n.Attrs, xml.Attr{Name: xml.Name{Local: "min"}, Value: strconv.Itoa(column)},
					xml.Attr{Name: xml.Name{Local: "max"}, Value: strconv.Itoa(column)})
				sort.Slice(n.Attrs, func(i, j int) bool {
					a, b := n.Attrs[i].Name, n.Attrs[j].Name
					return a.Space < b.Space || a.Space == b.Space && a.Local < b.Local
				})
				result = append(result, piece{Kind: "element", Node: n})
			}
			last = maximum
		}
		cols.Parts = result
	}
	return nil
}
