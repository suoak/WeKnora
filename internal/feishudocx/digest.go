// Package feishudocx provides a versioned content identity for Feishu DOCX exports.
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
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	algorithm   = "feishu-docx-package-v1"
	wordNS      = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	coreNS      = "http://schemas.openxmlformats.org/package/2006/metadata/core-properties"
	createdNS   = "http://purl.org/dc/terms/"
	maxArchive  = 64 << 20
	maxPart     = 32 << 20
	maxExpanded = 128 << 20
)

// ErrInvalidPackage contains no source filename, document text or credentials.
var ErrInvalidPackage = errors.New("invalid or unsupported Feishu DOCX package")

type partDigest struct{ Name, SHA256 string }

// CanonicalDOCXDigest covers every regular ZIP part. Only export-created time
// and numbering abstract-ID allocation are normalized, based on repeated export
// evidence. Body, relationships, images, tables, modified time and numbering
// semantics remain protected. Unsupported/malformed packages fail closed.
func CanonicalDOCXDigest(content []byte) (string, string, error) {
	if len(content) > maxArchive {
		return "", "", ErrInvalidPackage
	}
	z, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil || len(z.File) == 0 || len(z.File) > 4096 {
		return "", "", ErrInvalidPackage
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
			return "", "", ErrInvalidPackage
		}
		seen[strings.ToLower(clean)] = true
		if entry.FileInfo().IsDir() {
			continue
		}
		if !entry.Mode().IsRegular() || entry.UncompressedSize64 > maxPart ||
			entry.CompressedSize64 > uint64(len(content)) ||
			entry.UncompressedSize64 > entry.CompressedSize64*200+1024 {
			return "", "", ErrInvalidPackage
		}
		expanded += entry.UncompressedSize64
		if expanded > maxExpanded {
			return "", "", ErrInvalidPackage
		}
		reader, openErr := entry.Open()
		if openErr != nil {
			return "", "", ErrInvalidPackage
		}
		body, readErr := io.ReadAll(io.LimitReader(reader, maxPart+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || len(body) > maxPart || uint64(len(body)) != entry.UncompressedSize64 {
			return "", "", ErrInvalidPackage
		}
		var root *node
		if strings.HasSuffix(name, ".xml") || strings.HasSuffix(name, ".rels") {
			root, err = parseXML(body)
			if err != nil {
				return "", "", ErrInvalidPackage
			}
		}
		switch name {
		case "[Content_Types].xml":
			contentTypes := xml.Name{
				Space: "http://schemas.openxmlformats.org/package/2006/content-types", Local: "Types",
			}
			if root.Name != contentTypes {
				return "", "", ErrInvalidPackage
			}
		case "_rels/.rels":
			relationships := xml.Name{
				Space: "http://schemas.openxmlformats.org/package/2006/relationships", Local: "Relationships",
			}
			if root.Name != relationships {
				return "", "", ErrInvalidPackage
			}
		case "word/document.xml":
			if root.Name != (xml.Name{Space: wordNS, Local: "document"}) {
				return "", "", ErrInvalidPackage
			}
		case "docProps/core.xml":
			if root.Name != (xml.Name{Space: coreNS, Local: "coreProperties"}) {
				return "", "", ErrInvalidPackage
			}
			count := 0
			for i := range root.Parts {
				child := root.Parts[i].Node
				if child != nil && child.Name == (xml.Name{Space: createdNS, Local: "created"}) {
					count++
					var timestamp strings.Builder
					for _, value := range child.Parts {
						if value.Kind != "text" {
							return "", "", ErrInvalidPackage
						}
						timestamp.WriteString(value.Value)
					}
					if _, err := time.Parse(time.RFC3339Nano, timestamp.String()); err != nil {
						return "", "", ErrInvalidPackage
					}
					child.Parts = []piece{{Kind: "text", Value: "export-generated-created-time"}}
				}
			}
			if count > 1 {
				return "", "", ErrInvalidPackage
			}
			body, _ = json.Marshal(root)
		case "word/numbering.xml":
			body, err = normalizeNumbering(root)
			if err != nil {
				return "", "", ErrInvalidPackage
			}
		}
		parts = append(parts, partDigest{Name: name, SHA256: sum(body)})
	}
	for _, required := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml"} {
		found := false
		for _, part := range parts {
			found = found || part.Name == required
		}
		if !found {
			return "", "", ErrInvalidPackage
		}
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Name < parts[j].Name })
	identity, _ := json.Marshal(struct {
		Algorithm string
		Parts     []partDigest
	}{algorithm, parts})
	return sum(identity), algorithm, nil
}

func sum(body []byte) string { hash := sha256.Sum256(body); return hex.EncodeToString(hash[:]) }

type (
	piece struct {
		Kind  string
		Value string `json:",omitempty"`
		Node  *node  `json:",omitempty"`
	}
	node struct {
		Name  xml.Name
		Attrs []xml.Attr
		Parts []piece
	}
)

func parseXML(body []byte) (*node, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var stack []*node
	var root *node
	tokens := 0
	declarationSeen := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrInvalidPackage
		}
		tokens++
		if tokens > 1000000 {
			return nil, ErrInvalidPackage
		}
		switch t := token.(type) {
		case xml.StartElement:
			if len(stack) >= 128 {
				return nil, ErrInvalidPackage
			}
			n := &node{Name: t.Name, Attrs: append([]xml.Attr(nil), t.Attr...)}
			attrs := map[xml.Name]bool{}
			for _, attr := range n.Attrs {
				if attrs[attr.Name] {
					return nil, ErrInvalidPackage
				}
				attrs[attr.Name] = true
			}
			sort.Slice(n.Attrs, func(i, j int) bool {
				a, b := n.Attrs[i].Name, n.Attrs[j].Name
				return a.Space < b.Space || (a.Space == b.Space && a.Local < b.Local)
			})
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.Parts = append(parent.Parts, piece{Kind: "element", Node: n})
			} else if root != nil {
				return nil, ErrInvalidPackage
			} else {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, ErrInvalidPackage
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				n := stack[len(stack)-1]
				n.Parts = append(n.Parts, piece{Kind: "text", Value: string(t)})
			} else if strings.TrimSpace(string(t)) != "" {
				return nil, ErrInvalidPackage
			}
		case xml.Comment:
			if len(stack) > 0 {
				n := stack[len(stack)-1]
				n.Parts = append(n.Parts, piece{Kind: "comment", Value: string(t)})
			} else {
				return nil, ErrInvalidPackage
			}
		case xml.ProcInst:
			if t.Target != "xml" || root != nil || declarationSeen {
				return nil, ErrInvalidPackage
			}
			declarationSeen = true
		case xml.Directive:
			return nil, ErrInvalidPackage
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, ErrInvalidPackage
	}
	return root, nil
}

func withoutAttr(n *node, name xml.Name) (node, string, error) {
	normalized := *n
	normalized.Attrs = nil
	value := ""
	for _, attr := range n.Attrs {
		if attr.Name == name {
			value = attr.Value
		} else {
			normalized.Attrs = append(normalized.Attrs, attr)
		}
	}
	if value == "" || strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return node{}, "", ErrInvalidPackage
	}
	numeric, err := strconv.ParseUint(value, 10, 32)
	if err != nil || strconv.FormatUint(numeric, 10) != value {
		return node{}, "", ErrInvalidPackage
	}
	return normalized, value, nil
}

func normalizeNumbering(root *node) ([]byte, error) {
	if root == nil || root.Name != (xml.Name{Space: wordNS, Local: "numbering"}) {
		return nil, ErrInvalidPackage
	}
	abstracts := map[string]string{}
	var definitions []string
	for _, part := range root.Parts {
		if part.Node != nil && part.Node.Name == (xml.Name{Space: wordNS, Local: "abstractNum"}) {
			normalized, id, err := withoutAttr(part.Node, xml.Name{Space: wordNS, Local: "abstractNumId"})
			if err != nil || abstracts[id] != "" {
				return nil, ErrInvalidPackage
			}
			raw, _ := json.Marshal(normalized)
			abstracts[id] = sum(raw)
			definitions = append(definitions, abstracts[id])
		}
	}
	type instance struct {
		ID    string
		Value node
	}
	var instances []instance
	var other []piece
	seen := map[string]bool{}
	for _, part := range root.Parts {
		n := part.Node
		if n == nil || n.Name != (xml.Name{Space: wordNS, Local: "num"}) {
			if n == nil || n.Name != (xml.Name{Space: wordNS, Local: "abstractNum"}) {
				other = append(other, part)
			}
			continue
		}
		normalized, id, err := withoutAttr(n, xml.Name{Space: wordNS, Local: "numId"})
		if err != nil || seen[id] {
			return nil, ErrInvalidPackage
		}
		seen[id] = true
		normalized.Parts = append([]piece(nil), n.Parts...)
		refs := 0
		for i, p := range normalized.Parts {
			if p.Node != nil && p.Node.Name == (xml.Name{Space: wordNS, Local: "abstractNumId"}) {
				ref, target, err := withoutAttr(p.Node, xml.Name{Space: wordNS, Local: "val"})
				if err != nil || abstracts[target] == "" {
					return nil, ErrInvalidPackage
				}
				ref.Attrs = append(ref.Attrs, xml.Attr{
					Name: xml.Name{Space: wordNS, Local: "val"}, Value: abstracts[target],
				})
				normalized.Parts[i].Node = &ref
				refs++
			}
		}
		if refs != 1 {
			return nil, ErrInvalidPackage
		}
		instances = append(instances, instance{ID: id, Value: normalized})
	}
	sort.Strings(definitions)
	sort.Slice(instances, func(i, j int) bool { return instances[i].ID < instances[j].ID })
	return json.Marshal(struct {
		Name        xml.Name
		Attrs       []xml.Attr
		Definitions []string
		Instances   []instance
		Other       []piece
	}{root.Name, root.Attrs, definitions, instances, other})
}
