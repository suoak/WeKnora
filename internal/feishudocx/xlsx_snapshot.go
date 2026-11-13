package feishudocx

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"sort"
	"strings"
)

func writeSnapshotContentTypes(reader io.Reader, writer io.Writer) error {
	body, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return ErrInvalidXLSXPackage
	}
	root, err := parseXML(body)
	const ns = "http://schemas.openxmlformats.org/package/2006/content-types"
	if err != nil || root.Name != (xml.Name{Space: ns, Local: "Types"}) {
		return ErrInvalidXLSXPackage
	}
	parts := map[string]piece{}
	for _, part := range root.Parts {
		if part.Kind == "text" && strings.TrimSpace(part.Value) == "" {
			continue
		}
		n := part.Node
		if n == nil || n.Name.Space != ns || len(n.Parts) != 0 || len(n.Attrs) != 2 ||
			(n.Name.Local != "Default" && n.Name.Local != "Override") {
			return ErrInvalidXLSXPackage
		}
		field := "Extension"
		if n.Name.Local == "Override" {
			field = "PartName"
		}
		key, media := "", ""
		for _, attr := range n.Attrs {
			if attr.Name.Space != "" {
				return ErrInvalidXLSXPackage
			}
			switch attr.Name.Local {
			case field:
				key = attr.Value
			case "ContentType":
				media = attr.Value
			default:
				return ErrInvalidXLSXPackage
			}
		}
		if key == "" || media == "" {
			return ErrInvalidXLSXPackage
		}
		id := n.Name.Local + ":" + strings.ToLower(key)
		if _, duplicate := parts[id]; duplicate {
			return ErrInvalidXLSXPackage
		}
		parts[id] = part
	}
	keys := make([]string, 0, len(parts))
	for key := range parts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	root.Parts = nil
	for _, key := range keys {
		root.Parts = append(root.Parts, parts[key])
	}
	return json.NewEncoder(writer).Encode(root)
}
