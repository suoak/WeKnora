package service

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"net/url"
	"path"
	"strings"
)

// The initial exact-file path proves only this archive's bytes. It cannot
// authorize an external relationship, an embedded package, or a parser that
// follows one. Check before invoking the native parser or any image resolver.
func checkExactDOCXDependencies(data []byte) error {
	return checkExactDOCXGraph(data, nil)
}

func checkExactDOCXGraph(data []byte, packages map[string][]byte) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(archive.File) == 0 || len(archive.File) > 4096 {
		return errPersonalExactDocument
	}
	seen := make(map[string]bool, len(archive.File))
	available := make(map[string]bool, len(archive.File))
	for _, entry := range archive.File {
		available[strings.ToLower(entry.Name)] = true
	}
	var expanded uint64
	packageReferences := map[string]int{}
	literalHyperlinks := map[string]bool{}
	for _, entry := range archive.File {
		name := strings.ToLower(entry.Name)
		if name == "" || name == "." || name == ".." || strings.HasPrefix(name, "../") ||
			strings.Contains(name, "\\") || strings.HasPrefix(name, "/") ||
			path.Clean(name) != strings.TrimSuffix(name, "/") || seen[name] ||
			strings.HasSuffix(name, "vbaproject.bin") {
			return errPersonalExactDocument
		}
		seen[name] = true
		if entry.UncompressedSize64 > 64<<20 || expanded > (128<<20)-entry.UncompressedSize64 {
			return errPersonalExactDocument
		}
		expanded += entry.UncompressedSize64
		if strings.HasPrefix(name, "word/embeddings/") {
			if packages == nil || !strings.HasSuffix(name, ".xlsx") ||
				len(packages) >= 4 || entry.UncompressedSize64 > 8<<20 {
				return errPersonalExactDocument
			}
			reader, err := entry.Open()
			if err != nil {
				return errPersonalExactDocument
			}
			body, err := io.ReadAll(io.LimitReader(reader, (8<<20)+1))
			_ = reader.Close()
			if err != nil || len(body) > 8<<20 || uint64(len(body)) != entry.UncompressedSize64 {
				return errPersonalExactDocument
			}
			packages[name] = body
		}
		if !strings.HasSuffix(name, ".rels") {
			continue
		}
		if entry.UncompressedSize64 > 1<<20 {
			return errPersonalExactDocument
		}
		reader, openErr := entry.Open()
		if openErr != nil {
			return errPersonalExactDocument
		}
		var relationships struct {
			XMLName xml.Name `xml:"Relationships"`
			Rows    []struct {
				ID     string `xml:"Id,attr"`
				Mode   string `xml:"TargetMode,attr"`
				Target string `xml:"Target,attr"`
				Type   string `xml:"Type,attr"`
			} `xml:"Relationship"`
		}
		err = xml.NewDecoder(io.LimitReader(reader, (1<<20)+1)).Decode(&relationships)
		_ = reader.Close()
		if err != nil {
			return errPersonalExactDocument
		}
		relationshipIDs := map[string]bool{}
		for _, relationship := range relationships.Rows {
			if relationship.ID != "" {
				if relationshipIDs[relationship.ID] {
					return errPersonalExactDocument
				}
				relationshipIDs[relationship.ID] = true
			}
			// A hyperlink is source-file text, not authority to fetch its target.
			// The pinned native conversion is tested to preserve both label and
			// URL without opening a connection. Media/packages remain fail-closed.
			if relationship.Mode == "External" && relationship.Type ==
				"http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" {
				target, parseErr := url.Parse(relationship.Target)
				if name != "word/_rels/document.xml.rels" || relationship.ID == "" ||
					parseErr != nil || target.User != nil || target.Hostname() == "" ||
					(target.Scheme != "https" && target.Scheme != "http") {
					return errPersonalExactDocument
				}
				literalHyperlinks[relationship.ID] = true
				continue
			}
			if relationship.Mode != "" && relationship.Mode != "Internal" ||
				strings.Contains(relationship.Target, ":") || strings.HasPrefix(relationship.Target, "//") ||
				strings.HasSuffix(relationship.Type, "/oleObject") {
				return errPersonalExactDocument
			}
			target, decodeErr := url.PathUnescape(relationship.Target)
			if decodeErr != nil || target == "" || strings.ContainsAny(target, "\\\x00:?#") {
				return errPersonalExactDocument
			}
			base := path.Dir(path.Dir(name))
			if strings.HasPrefix(target, "/") {
				base = "." // OPC package-absolute part, never a host file path.
			}
			resolved := strings.ToLower(path.Clean(path.Join(base, strings.TrimPrefix(target, "/"))))
			if resolved == ".." || strings.HasPrefix(resolved, "../") || !available[resolved] {
				return errPersonalExactDocument
			}
			if strings.HasSuffix(relationship.Type, "/package") {
				if packages == nil || name != "word/_rels/document.xml.rels" ||
					relationship.Type !=
						"http://schemas.openxmlformats.org/officeDocument/2006/relationships/package" ||
					!strings.HasPrefix(resolved, "word/embeddings/") || !strings.HasSuffix(resolved, ".xlsx") {
					return errPersonalExactDocument
				}
				packageReferences[resolved]++
			}
		}
	}
	if err := checkExactLiteralHyperlinkUses(archive, literalHyperlinks); err != nil {
		return err
	}
	if len(packageReferences) != len(packages) {
		return errPersonalExactDocument
	}
	for name := range packages {
		if packageReferences[name] != 1 {
			return errPersonalExactDocument
		}
	}
	return nil
}

// Never trust the relationship type alone: an image/object could reference the
// same ID and cause a converter to treat its target as a fetchable dependency.
func checkExactLiteralHyperlinkUses(archive *zip.Reader, ids map[string]bool) error {
	if len(ids) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, entry := range archive.File {
		if !strings.HasSuffix(strings.ToLower(entry.Name), ".xml") {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return errPersonalExactDocument
		}
		decoder := xml.NewDecoder(io.LimitReader(reader, (64<<20)+1))
		for {
			token, readErr := decoder.Token()
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				_ = reader.Close()
				return errPersonalExactDocument
			}
			start, ok := token.(xml.StartElement)
			if !ok {
				continue
			}
			for _, attribute := range start.Attr {
				if !ids[attribute.Value] {
					continue
				}
				if strings.ToLower(entry.Name) != "word/document.xml" || start.Name.Local != "hyperlink" ||
					start.Name.Space != "http://schemas.openxmlformats.org/wordprocessingml/2006/main" ||
					attribute.Name.Local != "id" ||
					attribute.Name.Space != "http://schemas.openxmlformats.org/officeDocument/2006/relationships" {
					_ = reader.Close()
					return errPersonalExactDocument
				}
				seen[attribute.Value] = true
			}
		}
		_ = reader.Close()
	}
	if len(seen) != len(ids) {
		return errPersonalExactDocument
	}
	return nil
}
