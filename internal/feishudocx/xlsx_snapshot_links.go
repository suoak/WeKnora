package feishudocx

import (
	"archive/zip"
	"encoding/json"
	"encoding/xml"
	"io"
	"path"
	"sort"
	"strings"
)

const hyperlinkRelNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

// Exporters allocate hyperlink relationship IDs and declaration order afresh.
// Bind the complete resolved relationship and every hyperlink attribute instead.
// Other relationship IDs stay unchanged; unexpected uses of a remapped ID fail.
func snapshotHyperlinkParts(z *zip.Reader) (map[string][]byte, map[string]map[string]string, error) {
	canonical, owners := map[string][]byte{}, map[string]map[string]string{}
	total := 0
	for _, file := range z.File {
		name := file.Name
		if !strings.HasPrefix(name, "xl/worksheets/_rels/") || !strings.HasSuffix(name, ".xml.rels") {
			continue
		}
		if file.UncompressedSize64 > 1<<20 {
			return nil, nil, ErrInvalidXLSXPackage
		}
		reader, err := file.Open()
		if err != nil {
			return nil, nil, ErrInvalidXLSXPackage
		}
		body, readErr := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
		closeErr := reader.Close()
		total += len(body)
		if readErr != nil || closeErr != nil || len(body) > 1<<20 || total > 4<<20 ||
			uint64(len(body)) != file.UncompressedSize64 {
			return nil, nil, ErrInvalidXLSXPackage
		}
		root, aliases, err := normalizeHyperlinkRelationships(body)
		if err != nil {
			return nil, nil, err
		}
		if len(aliases) == 0 {
			continue
		}
		canonical[name], _ = json.Marshal(root)
		owner := path.Join(path.Dir(path.Dir(name)), strings.TrimSuffix(path.Base(name), ".rels"))
		owners[owner] = aliases
	}
	return canonical, owners, nil
}

func normalizeHyperlinkRelationships(body []byte) (*node, map[string]string, error) {
	const ns = "http://schemas.openxmlformats.org/package/2006/relationships"
	root, err := parseXML(body)
	if err != nil || root.Name != (xml.Name{Space: ns, Local: "Relationships"}) || len(root.Parts) > 8192 {
		return nil, nil, ErrInvalidXLSXPackage
	}
	ids, aliases := map[string]bool{}, map[string]string{}
	parts := []piece{}
	for _, part := range root.Parts {
		if part.Kind == "text" && strings.TrimSpace(part.Value) == "" {
			continue
		}
		n := part.Node
		if n == nil || n.Name != (xml.Name{Space: ns, Local: "Relationship"}) ||
			len(n.Parts) != 0 || len(n.Attrs) < 3 || len(n.Attrs) > 4 {
			return nil, nil, ErrInvalidXLSXPackage
		}
		values, idIndex := map[string]string{}, -1
		for i, attr := range n.Attrs {
			if attr.Name.Space != "" {
				return nil, nil, ErrInvalidXLSXPackage
			}
			switch attr.Name.Local {
			case "Id":
				idIndex = i
			case "Type", "Target", "TargetMode":
			default:
				return nil, nil, ErrInvalidXLSXPackage
			}
			values[attr.Name.Local] = attr.Value
		}
		id := values["Id"]
		if id == "" || ids[id] || strings.HasPrefix(id, "snapshot-hyperlink-") ||
			values["Type"] == "" || values["Target"] == "" {
			return nil, nil, ErrInvalidXLSXPackage
		}
		ids[id] = true
		if values["Type"] == hyperlinkRelNS+"/hyperlink" {
			delete(values, "Id")
			identity, _ := json.Marshal(values)
			alias := "snapshot-hyperlink-" + sum(identity)
			aliases[id], n.Attrs[idIndex].Value = alias, alias
		}
		parts = append(parts, part)
	}
	sort.Slice(parts, func(i, j int) bool {
		a, _ := json.Marshal(parts[i])
		b, _ := json.Marshal(parts[j])
		return string(a) < string(b)
	})
	root.Parts = parts
	return root, aliases, nil
}

func normalizeSnapshotHyperlinks(root *node) error {
	refs, parts := map[string]bool{}, []piece{}
	for _, part := range root.Parts {
		if part.Kind == "text" && strings.TrimSpace(part.Value) == "" {
			continue
		}
		n := part.Node
		if n == nil || n.Name != (xml.Name{Space: spreadsheetNS, Local: "hyperlink"}) || len(n.Parts) != 0 {
			return ErrInvalidXLSXPackage
		}
		ref := ""
		for _, attr := range n.Attrs {
			if attr.Name == (xml.Name{Local: "ref"}) {
				ref = strings.ToLower(attr.Value)
			}
		}
		if ref == "" || refs[ref] || len(parts) >= 4096 {
			return ErrInvalidXLSXPackage
		}
		refs[ref] = true
		parts = append(parts, part)
	}
	sort.Slice(parts, func(i, j int) bool {
		a, _ := json.Marshal(parts[i])
		b, _ := json.Marshal(parts[j])
		return string(a) < string(b)
	})
	root.Parts = parts
	return nil
}
