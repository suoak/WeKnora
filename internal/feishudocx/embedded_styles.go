package feishudocx

import (
	"encoding/json"
	"encoding/xml"
	"sort"
	"strconv"
	"strings"
)

// normalizeEmbeddedStyleIndexes changes only indexed table allocation. Full
// border/fill/style definitions, multiplicity and each resolved cell style bind.
// Index zero stays fixed because omitted style attributes implicitly select it.
func normalizeEmbeddedStyleIndexes(parts map[string][]byte) (map[string][]byte, error) {
	body, ok := parts["xl/styles.xml"]
	if !ok {
		return nil, nil
	}
	root, err := parseXML(body)
	if err != nil || root.Name != (xml.Name{Space: spreadsheetNS, Local: "styleSheet"}) {
		return nil, ErrInvalidPackage
	}
	tables := map[string]*node{}
	for _, part := range root.Parts {
		if n := part.Node; n != nil && n.Name.Space == spreadsheetNS {
			if tables[n.Name.Local] != nil {
				return nil, ErrInvalidPackage
			}
			tables[n.Name.Local] = n
		}
	}
	borders, err := reorderStyleTable(tables["borders"], "border")
	if err != nil {
		return nil, err
	}
	if err := remapStyleReferences(root, "borderId", "xf", borders); err != nil {
		return nil, err
	}
	if tables["fills"] != nil {
		fills, err := reorderStyleTable(tables["fills"], "fill")
		if err != nil {
			return nil, err
		}
		if err := remapStyleReferences(root, "fillId", "xf", fills); err != nil {
			return nil, err
		}
	}
	styles, err := reorderStyleTable(tables["cellXfs"], "xf")
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	out["xl/styles.xml"], err = json.Marshal(root)
	if err != nil {
		return nil, err
	}
	for name, body := range parts {
		if !strings.HasPrefix(name, "xl/worksheets/") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		sheet, err := parseXML(body)
		if err != nil || sheet.Name != (xml.Name{Space: spreadsheetNS, Local: "worksheet"}) {
			return nil, ErrInvalidPackage
		}
		if err := remapStyleReferences(sheet, "s", "c,row", styles); err != nil {
			return nil, err
		}
		if err := remapStyleReferences(sheet, "style", "col", styles); err != nil {
			return nil, err
		}
		out[name], err = json.Marshal(sheet)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func reorderStyleTable(table *node, element string) (map[string]string, error) {
	if table == nil || len(table.Parts) == 0 || len(table.Parts) > 4096 {
		return nil, ErrInvalidPackage
	}
	type entry struct {
		part piece
		key  string
		old  int
	}
	entries := make([]entry, 0, len(table.Parts))
	for i, part := range table.Parts {
		if part.Kind != "element" || part.Node == nil ||
			part.Node.Name != (xml.Name{Space: spreadsheetNS, Local: element}) {
			return nil, ErrInvalidPackage
		}
		body, err := json.Marshal(part.Node)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry{part: part, key: string(body), old: i})
	}
	for _, attr := range table.Attrs {
		if attr.Name == (xml.Name{Local: "count"}) && attr.Value != strconv.Itoa(len(entries)) {
			return nil, ErrInvalidPackage
		}
	}
	sort.SliceStable(entries[1:], func(i, j int) bool { return entries[i+1].key < entries[j+1].key })
	indexes, first := map[string]string{}, map[string]string{}
	for i, entry := range entries {
		index := strconv.Itoa(i)
		if previous, ok := first[entry.key]; ok {
			index = previous
		} else {
			first[entry.key] = index
		}
		indexes[strconv.Itoa(entry.old)] = index
		table.Parts[i] = entry.part
	}
	return indexes, nil
}

func remapStyleReferences(n *node, field, elements string, indexes map[string]string) error {
	for i, attr := range n.Attrs {
		if attr.Name != (xml.Name{Local: field}) {
			continue
		}
		if n.Name.Space != spreadsheetNS || !strings.Contains(
			","+elements+",", ","+n.Name.Local+",",
		) {
			return ErrInvalidPackage
		}
		index, ok := indexes[attr.Value]
		if !ok {
			return ErrInvalidPackage
		}
		n.Attrs[i].Value = index
	}
	for _, part := range n.Parts {
		if part.Node != nil {
			if err := remapStyleReferences(part.Node, field, elements, indexes); err != nil {
				return err
			}
		}
	}
	return nil
}
