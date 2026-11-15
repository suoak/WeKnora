package feishudocx

import (
	"encoding/json"
	"encoding/xml"
	"sort"
	"strconv"
	"strings"
)

// Non-overlapping column definitions commute. Preserve every attribute and the
// original order of overlapping ranges, whose order may affect effective style.
func normalizeEmbeddedColumnOrder(worksheet *node) error {
	blocks := 0
	for _, part := range worksheet.Parts {
		cols := part.Node
		if cols == nil || cols.Name != (xml.Name{Space: spreadsheetNS, Local: "cols"}) {
			continue
		}
		blocks++
		if blocks > 1 || len(cols.Parts) > 32768 {
			return ErrInvalidPackage
		}
		type definition struct {
			part piece
			min  int
			max  int
		}
		var entries []definition
		var positions []int
		for position, part := range cols.Parts {
			if part.Kind == "text" && strings.TrimSpace(part.Value) == "" {
				continue
			}
			col := part.Node
			if col == nil || col.Name != (xml.Name{Space: spreadsheetNS, Local: "col"}) || len(col.Parts) != 0 {
				return ErrInvalidPackage
			}
			entry := definition{part: part}
			for _, attr := range col.Attrs {
				if attr.Name.Space != "" || (attr.Name.Local != "min" && attr.Name.Local != "max") {
					continue
				}
				value, err := strconv.Atoi(attr.Value)
				if err != nil || value < 1 || value > 16384 {
					return ErrInvalidPackage
				}
				if attr.Name.Local == "min" {
					entry.min = value
				} else {
					entry.max = value
				}
			}
			if entry.min == 0 || entry.max < entry.min || len(entries) >= 16384 {
				return ErrInvalidPackage
			}
			entries = append(entries, entry)
			positions = append(positions, position)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].min < entries[j].min })
		overlap := false
		for i := 1; i < len(entries); i++ {
			if entries[i].min <= entries[i-1].max {
				overlap = true
				break
			}
		}
		if !overlap {
			for i, entry := range entries {
				cols.Parts[positions[i]] = entry.part
			}
		}
	}
	return nil
}

func normalizeEmbeddedColumnsWithoutStyles(parts map[string][]byte) (map[string][]byte, error) {
	out := map[string][]byte{}
	for name, body := range parts {
		if !strings.HasPrefix(name, "xl/worksheets/") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		root, err := parseXML(body)
		if err != nil || root.Name != (xml.Name{Space: spreadsheetNS, Local: "worksheet"}) {
			return nil, ErrInvalidPackage
		}
		if err := normalizeEmbeddedColumnOrder(root); err != nil {
			return nil, err
		}
		out[name], err = json.Marshal(root)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
