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
	for _, entry := range archive.File {
		name := strings.ToLower(entry.Name)
		if name == "" || name == "." || name == ".." || strings.HasPrefix(name, "../") ||
			strings.Contains(name, "\\") || strings.HasPrefix(name, "/") ||
			path.Clean(name) != strings.TrimSuffix(name, "/") || seen[name] ||
			strings.HasPrefix(name, "word/embeddings/") || strings.HasSuffix(name, "vbaproject.bin") {
			return errPersonalExactDocument
		}
		seen[name] = true
		if entry.UncompressedSize64 > 64<<20 || expanded > (128<<20)-entry.UncompressedSize64 {
			return errPersonalExactDocument
		}
		expanded += entry.UncompressedSize64
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
		for _, relationship := range relationships.Rows {
			if relationship.Mode != "" && relationship.Mode != "Internal" ||
				strings.Contains(relationship.Target, ":") || strings.HasPrefix(relationship.Target, "//") ||
				strings.HasSuffix(relationship.Type, "/oleObject") || strings.HasSuffix(relationship.Type, "/package") {
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
		}
	}
	return nil
}
