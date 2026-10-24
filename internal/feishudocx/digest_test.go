package feishudocx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

const w = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

func exampleParts(created, abstract, definition, reference string) map[string]string {
	return map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"_rels/.rels":         `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
		"word/document.xml": `<w:document xmlns:w="` + w + `"><w:body><w:p>` +
			`<w:r><w:t>same body</w:t></w:r></w:p></w:body></w:document>`,
		"docProps/core.xml": `<cp:coreProperties ` +
			`xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" ` +
			`xmlns:dcterms="http://purl.org/dc/terms/"><dcterms:created>` + created +
			`</dcterms:created><dcterms:modified>source-edit</dcterms:modified></cp:coreProperties>`,
		"word/numbering.xml": `<w:numbering xmlns:w="` + w + `"><w:abstractNum w:abstractNumId="` + abstract +
			`"><w:lvl w:ilvl="0"><w:numFmt w:val="` + definition + `"/></w:lvl></w:abstractNum>` +
			`<w:num w:numId="1"><w:abstractNumId w:val="` + reference + `"/></w:num></w:numbering>`,
		"word/media/image.bin": "image bytes",
	}
}

func archive(t *testing.T, parts map[string]string, reverse bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	var names []string
	for name := range parts {
		names = append(names, name)
	}
	// Deliberately stable opposite ordering, container timestamps/compression differ.
	sort.Strings(names)
	if reverse {
		for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
			names[i], names[j] = names[j], names[i]
		}
	}
	for _, name := range names {
		method := uint16(zip.Deflate)
		date := time.Date(2026, 10, 23, 1, 0, 0, 0, time.UTC)
		if reverse {
			method = zip.Store
			date = date.Add(time.Minute)
		}
		f, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: date})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write([]byte(parts[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestEquivalentExportContainerAndNumbering(t *testing.T) {
	a := archive(t, exampleParts("2026-10-24T01:00:00Z", "17", "decimal", "17"), false)
	b := archive(t, exampleParts("2026-10-24T01:00:04Z", "999", "decimal", "999"), true)
	left, algorithm, err := CanonicalDOCXDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	right, other, err := CanonicalDOCXDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	if algorithm != "feishu-docx-package-v1" || other != algorithm || left != right {
		t.Fatal("equivalent exported package differs", left, right)
	}
}

func TestGenuineChangesRemainDifferent(t *testing.T) {
	base := exampleParts("2026-10-24T01:00:00Z", "17", "decimal", "17")
	want, _, err := CanonicalDOCXDigest(archive(t, base, false))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"word/document.xml", "word/media/image.bin", "docProps/core.xml", "word/numbering.xml", "_rels/.rels",
	} {
		t.Run(part, func(t *testing.T) {
			changed := exampleParts("2026-10-24T01:00:00Z", "17", "decimal", "17")
			switch part {
			case "word/document.xml":
				changed[part] = strings.ReplaceAll(changed[part], "same body", "changed body")
			case "docProps/core.xml":
				changed[part] = strings.ReplaceAll(changed[part], "source-edit", "different-source-edit")
			case "word/numbering.xml":
				changed[part] = strings.ReplaceAll(changed[part], "decimal", "bullet")
			case "_rels/.rels":
				changed[part] = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="r1" Target="different"/></Relationships>`
			default:
				changed[part] += "changed"
			}
			got, _, err := CanonicalDOCXDigest(archive(t, changed, false))
			if err != nil {
				t.Fatal(err)
			}
			if got == want {
				t.Fatal("content mutation ignored")
			}
		})
	}
}

func TestInvalidPackagesRejected(t *testing.T) {
	for _, kind := range []string{
		"invalid", "duplicate", "traversal", "alias", "doctype", "duplicate_id", "missing_ref", "bomb",
		"abstract_numeric_alias", "num_numeric_alias", "large_numeric_id", "duplicate_declaration",
		"invalid_utf8_name", "control_name",
	} {
		t.Run(kind, func(t *testing.T) {
			parts := exampleParts("2026-10-24T01:00:00Z", "17", "decimal", "17")
			switch kind {
			case "invalid":
				if _, _, err := CanonicalDOCXDigest([]byte("not zip")); err == nil {
					t.Fatal("invalid accepted")
				}
				return
			case "traversal":
				parts["../word/evil"] = "x"
			case "alias":
				parts["word/./document.xml"] = "x"
			case "doctype":
				parts["word/document.xml"] = `<!DOCTYPE x [<!ENTITY body "secret">]>` + parts["word/document.xml"]
			case "duplicate_id":
				parts["word/numbering.xml"] = strings.Replace(
					parts["word/numbering.xml"], `</w:numbering>`,
					`<w:abstractNum w:abstractNumId="17"/></w:numbering>`, 1,
				)
			case "missing_ref":
				parts["word/numbering.xml"] = strings.Replace(
					parts["word/numbering.xml"], `w:val="17"`, `w:val="404"`, 1,
				)
			case "bomb":
				parts["word/media/bomb.bin"] = strings.Repeat("a", 4*1024*1024)
			case "abstract_numeric_alias":
				parts["word/numbering.xml"] = strings.ReplaceAll(parts["word/numbering.xml"], `"17"`, `"017"`)
			case "num_numeric_alias":
				parts["word/numbering.xml"] = strings.ReplaceAll(
					parts["word/numbering.xml"], `w:numId="1"`, `w:numId="01"`,
				)
			case "large_numeric_id":
				parts["word/numbering.xml"] = strings.ReplaceAll(
					parts["word/numbering.xml"], `"17"`, `"99999999999999999999999999"`,
				)
			case "duplicate_declaration":
				parts["word/document.xml"] = `<?xml version="1.0"?><?xml version="1.0"?>` + parts["word/document.xml"]
			case "invalid_utf8_name":
				parts["word/"+string([]byte{0xff})+".bin"] = "x"
			case "control_name":
				parts["word/new\nline.bin"] = "x"
			case "duplicate":
				var buf bytes.Buffer
				z := zip.NewWriter(&buf)
				for i := 0; i < 2; i++ {
					f, err := z.Create("word/document.xml")
					if err != nil {
						t.Fatal(err)
					}
					if _, err = fmt.Fprint(f, "x"); err != nil {
						t.Fatal(err)
					}
				}
				if err := z.Close(); err != nil {
					t.Fatal(err)
				}
				if _, _, err := CanonicalDOCXDigest(buf.Bytes()); err == nil {
					t.Fatal("duplicate accepted")
				}
				return
			}
			if _, _, err := CanonicalDOCXDigest(archive(t, parts, false)); err == nil {
				t.Fatal("unsafe package accepted")
			}
		})
	}
}

func TestNumberingGraphSemanticsRemainProtected(t *testing.T) {
	parts := exampleParts("2026-10-24T01:00:00Z", "7", "decimal", "7")
	parts["word/numbering.xml"] = `<w:numbering xmlns:w="` + w + `">` +
		`<w:abstractNum w:abstractNumId="7"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/></w:lvl></w:abstractNum>` +
		`<w:abstractNum w:abstractNumId="8"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/></w:lvl></w:abstractNum>` +
		`<w:num w:numId="1"><w:abstractNumId w:val="7"/></w:num>` +
		`<w:num w:numId="2"><w:abstractNumId w:val="8"/></w:num></w:numbering>`
	want, _, err := CanonicalDOCXDigest(archive(t, parts, false))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"swap_semantic_mapping", "override", "unused_definition"} {
		t.Run(kind, func(t *testing.T) {
			changed := make(map[string]string)
			for name, value := range parts {
				changed[name] = value
			}
			body := parts["word/numbering.xml"]
			switch kind {
			case "swap_semantic_mapping":
				body = strings.ReplaceAll(body, `w:val="7"`, `w:val="swap"`)
				body = strings.ReplaceAll(body, `w:val="8"`, `w:val="7"`)
				body = strings.ReplaceAll(body, `w:val="swap"`, `w:val="8"`)
			case "override":
				body = strings.Replace(body, `</w:num>`, `<w:lvlOverride w:ilvl="0"/></w:num>`, 1)
			case "unused_definition":
				body = strings.Replace(body, `</w:numbering>`, `<w:abstractNum w:abstractNumId="9"/></w:numbering>`, 1)
			}
			changed["word/numbering.xml"] = body
			got, _, err := CanonicalDOCXDigest(archive(t, changed, false))
			if err != nil {
				t.Fatal(err)
			}
			if got == want {
				t.Fatal("numbering semantic change was ignored")
			}
		})
	}
}

func TestCreatedNamespaceNotConfused(t *testing.T) {
	parts := exampleParts("first", "7", "decimal", "7")
	parts["docProps/core.xml"] = strings.ReplaceAll(parts["docProps/core.xml"], createdNS, "urn:other-created")
	first, _, err := CanonicalDOCXDigest(archive(t, parts, false))
	if err != nil {
		t.Fatal(err)
	}
	parts["docProps/core.xml"] = strings.ReplaceAll(parts["docProps/core.xml"], "first", "second")
	second, _, err := CanonicalDOCXDigest(archive(t, parts, false))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("non-dcterms created text wrongly ignored")
	}
}
