package feishudocx

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// embeddedXLSXIdentity normalizes only the ZIP envelope. In particular, it does
// not use CanonicalXLSXDigest: an embedded workbook's widths, properties, XML
// serialization, relationships and all binary parts remain byte-for-byte bound.
// Expansion shares the outer DOCX budget; nested workbook assets stay opaque.
func embeddedXLSXIdentity(content []byte, expanded *uint64) ([]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil || len(z.File) == 0 || len(z.File) > 4096 {
		return nil, ErrInvalidPackage
	}
	seen := map[string]bool{}
	required := map[string]xml.Name{
		"[Content_Types].xml": {Space: "http://schemas.openxmlformats.org/package/2006/content-types", Local: "Types"},
		"_rels/.rels":         {Space: "http://schemas.openxmlformats.org/package/2006/relationships", Local: "Relationships"},
		"xl/workbook.xml":     {Space: spreadsheetNS, Local: "workbook"},
	}
	var parts []partDigest
	for _, entry := range z.File {
		name := entry.Name
		clean := strings.TrimSuffix(name, "/")
		if clean == "" || !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 ||
			strings.ContainsAny(name, "\\%?#:") || strings.HasPrefix(name, "/") || path.Clean(clean) != clean ||
			clean == ".." || strings.HasPrefix(clean, "../") || seen[strings.ToLower(clean)] {
			return nil, ErrInvalidPackage
		}
		seen[strings.ToLower(clean)] = true
		if entry.FileInfo().IsDir() {
			continue
		}
		if !entry.Mode().IsRegular() || entry.UncompressedSize64 > maxPart ||
			entry.CompressedSize64 > uint64(len(content)) ||
			entry.UncompressedSize64 > entry.CompressedSize64*200+1024 {
			return nil, ErrInvalidPackage
		}
		*expanded += entry.UncompressedSize64
		if *expanded > maxExpanded {
			return nil, ErrInvalidPackage
		}
		reader, openErr := entry.Open()
		if openErr != nil {
			return nil, ErrInvalidPackage
		}
		body, readErr := io.ReadAll(io.LimitReader(reader, maxPart+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || len(body) > maxPart || uint64(len(body)) != entry.UncompressedSize64 {
			return nil, ErrInvalidPackage
		}
		if strings.HasSuffix(name, ".xml") || strings.HasSuffix(name, ".rels") {
			root, parseErr := parseXML(body)
			if parseErr != nil {
				return nil, ErrInvalidPackage
			}
			if want, ok := required[name]; ok {
				if root.Name != want {
					return nil, ErrInvalidPackage
				}
				delete(required, name)
			}
		}
		parts = append(parts, partDigest{Name: name, SHA256: sum(body)})
	}
	if len(required) != 0 {
		return nil, ErrInvalidPackage
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Name < parts[j].Name })
	identity, err := json.Marshal(struct {
		Algorithm string
		Parts     []partDigest
	}{"feishu-embedded-xlsx-zip-v1", parts})
	return identity, err
}
