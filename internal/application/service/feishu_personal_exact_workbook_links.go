package service

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Increasing the workbook count must not multiply the per-workbook expansion
// limit. Check the whole parent budget before invoking any native conversion.
func checkExactWorkbookExpansionBudget(packages map[string][]byte) error {
	var expanded uint64
	for _, data := range packages {
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil || len(z.File) > 256 {
			return errExactWorkbookUnsupported
		}
		for _, entry := range z.File {
			if entry.UncompressedSize64 > 8<<20 || expanded+entry.UncompressedSize64 > 64<<20 {
				return errExactWorkbookUnsupported
			}
			expanded += entry.UncompressedSize64
		}
	}
	return nil
}

const exactWorkbookHyperlinkType = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"
const exactWorkbookRelNamespace = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

// Links are literal source metadata. Preserve cell, URL, display, tooltip and
// local destination without resolving any target or letting native media readers
// reinterpret an external hyperlink relationship as an image/object dependency.
func exactWorkbookHyperlinkNotes(parts map[string][]byte) (map[string]string, error) {
	notes := map[string]string{}
	for name := range parts {
		if strings.HasPrefix(name, "xl/worksheets/_rels/") && strings.HasSuffix(name, ".rels") {
			sheet := "xl/worksheets/" + strings.TrimSuffix(path.Base(name), ".rels")
			if parts[sheet] == nil {
				return nil, errExactWorkbookUnsupported
			}
		}
	}
	for name, body := range parts {
		if !strings.HasPrefix(name, "xl/worksheets/") || strings.Contains(name, "/_rels/") ||
			!strings.HasSuffix(name, ".xml") {
			continue
		}
		targets := map[string]string{}
		var rels struct {
			Rows []struct {
				ID     string `xml:"Id,attr"`
				Type   string `xml:"Type,attr"`
				Target string `xml:"Target,attr"`
				Mode   string `xml:"TargetMode,attr"`
			} `xml:"Relationship"`
		}
		relName := path.Dir(name) + "/_rels/" + path.Base(name) + ".rels"
		if relBody := parts[relName]; len(relBody) > 0 {
			if xml.Unmarshal(relBody, &rels) != nil {
				return nil, errExactWorkbookUnsupported
			}
			for _, r := range rels.Rows {
				if r.Mode == "External" {
					if r.Type != exactWorkbookHyperlinkType || targets[r.ID] != "" {
						return nil, errExactWorkbookUnsupported
					}
					targets[r.ID] = r.Target
				}
			}
		}
		used, cells := map[string]bool{}, map[string]bool{}
		links := map[string]map[string]string{}
		d := xml.NewDecoder(bytes.NewReader(body))
		for {
			token, err := d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, errExactWorkbookUnsupported
			}
			start, ok := token.(xml.StartElement)
			if !ok {
				continue
			}
			for _, a := range start.Attr {
				if a.Name.Space == exactWorkbookRelNamespace && targets[a.Value] != "" &&
					(start.Name.Local != "hyperlink" || a.Name.Local != "id") {
					return nil, errExactWorkbookUnsupported
				}
				if start.Name.Local == "c" && a.Name.Local == "r" {
					cells[a.Value] = true
				}
			}
			if start.Name.Local != "hyperlink" {
				continue
			}
			attrs := map[string]string{}
			seenAttrs := map[xml.Name]bool{}
			for _, a := range start.Attr {
				if seenAttrs[a.Name] {
					return nil, errExactWorkbookUnsupported
				}
				seenAttrs[a.Name] = true
				key := a.Name.Local
				if key == "id" {
					if a.Name.Space != exactWorkbookRelNamespace || targets[a.Value] == "" {
						return nil, errExactWorkbookUnsupported
					}
					used[a.Value] = true
					attrs["url"] = targets[a.Value]
				} else {
					if a.Name.Space != "" || (key != "ref" && key != "display" && key != "tooltip" && key != "location") {
						return nil, errExactWorkbookUnsupported
					}
					if _, exists := attrs[key]; exists {
						return nil, errExactWorkbookUnsupported
					}
					attrs[key] = a.Value
				}
			}
			ref := attrs["ref"]
			col, row, err := excelize.CellNameToCoordinates(ref)
			canonical, canonicalErr := excelize.CoordinatesToCellName(col, row)
			if err != nil || canonicalErr != nil || canonical != ref || col > 64 || row > 1024 ||
				links[ref] != nil || (attrs["url"] == "" && attrs["location"] == "") {
				return nil, errExactWorkbookUnsupported
			}
			links[ref] = attrs
		}
		if len(used) != len(targets) {
			return nil, errExactWorkbookUnsupported
		}
		refs := make([]string, 0, len(links))
		for ref := range links {
			if !cells[ref] {
				return nil, errExactWorkbookUnsupported
			}
			refs = append(refs, ref)
		}
		sort.Strings(refs)
		var text strings.Builder
		escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#13;", "\n", "&#10;", "|", "&#124;")
		for _, ref := range refs {
			text.WriteString("\nHyperlink cell " + ref + ":")
			for _, key := range []string{"url", "location", "display", "tooltip"} {
				if value := links[ref][key]; value != "" {
					text.WriteString(" " + key + "=" + escape.Replace(value) + ";")
				}
			}
			text.WriteString("\n")
		}
		notes[name] = text.String()
	}
	return notes, nil
}
