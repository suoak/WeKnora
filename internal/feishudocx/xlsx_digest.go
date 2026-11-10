package feishudocx

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

const (
	maxXLSXPart     = 128 << 20
	maxXLSXExpanded = 256 << 20
)

const spreadsheetNS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"

// ErrInvalidXLSXPackage rejects unsupported structures without exposing content.
var ErrInvalidXLSXPackage = errors.New("invalid or unsupported Feishu XLSX package")

// CanonicalXLSXDigest uses a bounded streaming OOXML decoder without changing DOCX's
// identity contract. Repeated authorized Sheet exports vary column widths and
// grouping, despite identical cells, formulas, relationships and binary assets.
// Only width is omitted; column coverage and every other property remain bound.
// All ZIP members are included. Overlapping columns retain every attribute
// layer in source order; ambiguous/unsupported structures fail closed.
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
		if !entry.Mode().IsRegular() || entry.UncompressedSize64 > maxXLSXPart ||
			entry.CompressedSize64 > uint64(len(content)) ||
			entry.UncompressedSize64 > entry.CompressedSize64*200+1024 {
			return "", "", ErrInvalidXLSXPackage
		}
		expanded += entry.UncompressedSize64
		if expanded > maxXLSXExpanded {
			return "", "", ErrInvalidXLSXPackage
		}
		reader, e := entry.Open()
		if e != nil {
			return "", "", ErrInvalidXLSXPackage
		}
		hash := sha256.New()
		bounded := &io.LimitedReader{R: reader, N: maxXLSXPart + 1}
		var readErr error
		if strings.HasSuffix(name, ".xml") || strings.HasSuffix(name, ".rels") {
			var required xml.Name
			switch name {
			case "[Content_Types].xml":
				required = xml.Name{
					Space: "http://schemas.openxmlformats.org/package/2006/content-types", Local: "Types",
				}
			case "_rels/.rels":
				required = xml.Name{
					Space: "http://schemas.openxmlformats.org/package/2006/relationships", Local: "Relationships",
				}
			case "xl/workbook.xml":
				required = xml.Name{Space: spreadsheetNS, Local: "workbook"}
			}
			readErr = streamXLSXXML(bounded, hash, required)
		} else {
			_, readErr = io.Copy(hash, bounded)
		}
		closeErr := reader.Close()
		readBytes := int64(maxXLSXPart+1) - bounded.N
		if readErr != nil || closeErr != nil || readBytes > maxXLSXPart ||
			uint64(readBytes) != entry.UncompressedSize64 {
			return "", "", ErrInvalidXLSXPackage
		}
		parts = append(parts, partDigest{Name: name, SHA256: hex.EncodeToString(hash.Sum(nil))})
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
		type columnRange struct {
			minimum, maximum int
			attrs            []xml.Attr
		}
		var ranges []columnRange
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
					// Feishu exports can contain layout widths above 255. Width is
					// already omitted from identity and never controls expansion;
					// retain numeric validation without the old 255-unit cap.
					if e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
						return ErrInvalidXLSXPackage
					}
				default:
					attrs = append(attrs, attr)
				}
			}
			if minimum < 1 || maximum < minimum {
				return ErrInvalidXLSXPackage
			}
			ranges = append(ranges, columnRange{minimum, maximum, attrs})
		}
		// Expand range packaging without choosing a winning overlapping style.
		// Every column retains its complete ordered stack, so reordering two
		// overlapping definitions or changing any non-width attribute changes
		// identity. Non-overlapping v1 identities remain byte-for-byte stable.
		columns := map[int][][]xml.Attr{}
		last, expanded := 0, 0
		for _, definition := range ranges {
			expanded += definition.maximum - definition.minimum + 1
			if expanded > 32768 {
				return ErrInvalidXLSXPackage
			}
			for column := definition.minimum; column <= definition.maximum; column++ {
				columns[column] = append(columns[column], definition.attrs)
			}
			if definition.maximum > last {
				last = definition.maximum
			}
		}
		var result []piece
		for column := 1; column <= last; column++ {
			for _, attrs := range columns[column] {
				n := &node{
					Name:  xml.Name{Space: spreadsheetNS, Local: "col"},
					Attrs: append([]xml.Attr(nil), attrs...),
				}
				n.Attrs = append(n.Attrs, xml.Attr{Name: xml.Name{Local: "min"}, Value: strconv.Itoa(column)},
					xml.Attr{Name: xml.Name{Local: "max"}, Value: strconv.Itoa(column)})
				sort.Slice(n.Attrs, func(i, j int) bool {
					a, b := n.Attrs[i].Name, n.Attrs[j].Name
					return a.Space < b.Space || a.Space == b.Space && a.Local < b.Local
				})
				result = append(result, piece{Kind: "element", Node: n})
			}
		}
		cols.Parts = result
	}
	return nil
}
