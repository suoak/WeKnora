package service

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func standaloneTestWorkbook(t *testing.T, edit func(map[string][]byte)) []byte {
	t.Helper()
	file := excelize.NewFile()
	defer func() { require.NoError(t, file.Close()) }()
	require.NoError(t, file.SetCellStr("Sheet1", "A1", "first sheet"))
	_, err := file.NewSheet("Hidden")
	require.NoError(t, err)
	require.NoError(t, file.SetCellStr("Hidden", "B2", "hidden value"))
	require.NoError(t, file.SetSheetVisible("Hidden", false))
	_, err = file.NewSheet("Empty")
	require.NoError(t, err)
	buffer, err := file.WriteToBuffer()
	require.NoError(t, err)
	archive, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	require.NoError(t, err)
	parts := map[string][]byte{}
	for _, entry := range archive.File {
		reader, err := entry.Open()
		require.NoError(t, err)
		parts[entry.Name], err = io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
	}
	if edit != nil {
		edit(parts)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	keys := make([]string, 0, len(parts))
	for key := range parts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry, err := writer.Create(key)
		require.NoError(t, err)
		_, err = entry.Write(parts[key])
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return output.Bytes()
}

func appendStandaloneTestXML(parts map[string][]byte, name, closing, extra string) {
	parts[name] = []byte(strings.Replace(string(parts[name]), closing, extra+closing, 1))
}

func standaloneTestDynamicMetadata(parts map[string][]byte) {
	parts["xl/metadata.xml"] = []byte(`<metadata xmlns="` + standaloneMainNS + `">
<metadataTypes count="1"><metadataType name="XLDAPR" cellMeta="1"/></metadataTypes>
<futureMetadata name="XLDAPR" count="1"><bk><extLst><ext uri="test">
<dynamicArrayProperties xmlns="http://schemas.microsoft.com/office/spreadsheetml/2017/dynamicarray"
fDynamic="1" fCollapsed="0"/></ext></extLst></bk></futureMetadata>
<cellMetadata count="1"><bk><rc t="1" v="0"/></bk></cellMetadata></metadata>`)
	appendStandaloneTestXML(parts, "xl/_rels/workbook.xml.rels", "</Relationships>",
		`<Relationship Id="dynamic" Type="`+exactWorkbookRelNamespace+`/sheetMetadata" Target="metadata.xml"/>`)
	appendStandaloneTestXML(parts, "[Content_Types].xml", "</Types>",
		`<Override PartName="/xl/metadata.xml" ContentType="`+
			`application/vnd.openxmlformats-officedocument.spreadsheetml.sheetMetadata+xml"/>`)
}

func TestExactStandaloneWorkbookMapsRelationsAndPreservesHiddenAndEmptySheets(t *testing.T) {
	data := standaloneTestWorkbook(t, func(parts map[string][]byte) {
		parts["xl/worksheets/sheet7.xml"] = parts["xl/worksheets/sheet2.xml"]
		delete(parts, "xl/worksheets/sheet2.xml")
		for _, name := range []string{"xl/_rels/workbook.xml.rels", "[Content_Types].xml"} {
			parts[name] = bytes.ReplaceAll(parts[name], []byte("sheet2.xml"), []byte("sheet7.xml"))
		}
	})
	parsed, err := parseExactStandaloneWorkbook(context.Background(), "source.xlsx", data)
	require.NoError(t, err)
	sheets := map[string]string{}
	for _, section := range parsed.Sections {
		sheets[section.sheet] += section.markdown
		if section.sheet == "Hidden" {
			require.Equal(t, "xl/worksheets/sheet7.xml", section.name)
		}
	}
	require.Len(t, sheets, 3)
	require.Contains(t, sheets["Sheet1"], "first sheet")
	require.Contains(t, sheets["Hidden"], "hidden value")
	require.Contains(t, sheets["Hidden"], "state=hidden")
	require.Contains(t, sheets["Empty"], "| Row |")
}

func TestExactStandaloneArrayPreservesSavedCacheWithoutEvaluation(t *testing.T) {
	data := standaloneTestWorkbook(t, func(parts map[string][]byte) {
		standaloneTestDynamicMetadata(parts)
		parts["xl/worksheets/sheet1.xml"] = []byte(`<worksheet xmlns="` + standaloneMainNS + `">
<sheetData><row r="1"><c r="A1" cm="1"><f t="array" ref="A1:A1">SEQUENCE(1)</f><v>7</v></c>
</row></sheetData></worksheet>`)
	})
	parsed, err := parseExactStandaloneWorkbook(context.Background(), "source.xlsx", data)
	require.NoError(t, err)
	var text string
	for _, section := range parsed.Sections {
		if section.sheet == "Sheet1" {
			text += section.markdown
		}
	}
	require.Contains(t, text, "| 1 | 7 |")
	require.Contains(t, text, "SEQUENCE(1)")
	require.Contains(t, text, "not recalculated")
	require.Contains(t, text, "Cell A1: metadata record 1")
	_, err = exactWorkbookParts(data)
	require.NoError(t, err)
	// Existing embedded consumers still reject array semantics.
	file, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer func() { require.NoError(t, file.Close()) }()
	parts, err := exactWorkbookParts(data)
	require.NoError(t, err)
	_, err = exactWorkbookFormulaNotes(file, "Sheet1", parts["xl/worksheets/sheet1.xml"])
	require.ErrorIs(t, err, errExactWorkbookUnsupported)
}

func TestExactStandaloneRejectsUnknownOrAmbiguousInputs(t *testing.T) {
	for _, change := range []string{
		"orphan worksheet", "wrong root", "missing metadata", "dangling metadata", "external image",
		"unknown MIME", "duplicate document", "unknown XML", "macro",
		"nested relationship", "duplicate root role", "duplicate cached value", "misplaced cell",
	} {
		t.Run(change, func(t *testing.T) {
			data := standaloneTestWorkbook(t, func(parts map[string][]byte) {
				switch change {
				case "orphan worksheet":
					parts["xl/worksheets/sheet99.xml"] = parts["xl/worksheets/sheet1.xml"]
				case "wrong root":
					parts["_rels/.rels"] = bytes.ReplaceAll(parts["_rels/.rels"],
						[]byte("xl/workbook.xml"), []byte("xl/worksheets/sheet1.xml"))
				case "missing metadata", "dangling metadata":
					parts["xl/worksheets/sheet1.xml"] = bytes.Replace(parts["xl/worksheets/sheet1.xml"],
						[]byte(`<c r="A1"`), []byte(`<c r="A1" cm="2"`), 1)
					if change == "dangling metadata" {
						standaloneTestDynamicMetadata(parts)
					}
				case "external image":
					appendStandaloneTestXML(parts, "xl/_rels/workbook.xml.rels", "</Relationships>",
						`<Relationship Id="remote" Type="`+exactWorkbookRelNamespace+
							`/image" Target="https://example.invalid/private.png" TargetMode="External"/>`)
				case "unknown MIME":
					parts["[Content_Types].xml"] = bytes.ReplaceAll(parts["[Content_Types].xml"],
						[]byte("application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"),
						[]byte("application/x-unknown-input"))
				case "duplicate document":
					parts["xl/worksheets/sheet1.xml"] = append(parts["xl/worksheets/sheet1.xml"],
						[]byte(`<worksheet xmlns="`+standaloneMainNS+`"><sheetData/></worksheet>`)...)
				case "unknown XML":
					appendStandaloneTestXML(parts, "xl/worksheets/sheet1.xml", "</worksheet>", "<unknownData/>")
				case "macro":
					parts["xl/vbaProject.bin"] = []byte("opaque input")
				case "nested relationship":
					parts["_rels/.rels"] = bytes.Replace(parts["_rels/.rels"], []byte("</Relationships>"),
						[]byte(`<Relationships/></Relationships>`), 1)
				case "duplicate root role":
					appendStandaloneTestXML(parts, "_rels/.rels", "</Relationships>",
						`<Relationship Id="otherBook" Type="`+exactWorkbookRelNamespace+
							`/officeDocument" Target="xl/workbook.xml"/>`)
				case "duplicate cached value":
					parts["xl/worksheets/sheet1.xml"] = bytes.Replace(parts["xl/worksheets/sheet1.xml"],
						[]byte("</c>"), []byte("<v>99</v></c>"), 1)
				case "misplaced cell":
					appendStandaloneTestXML(parts, "xl/worksheets/sheet1.xml", "</worksheet>",
						`<c r="C3"><v>ignored value</v></c>`)
				}
			})
			_, err := parseExactStandaloneWorkbook(context.Background(), "source.xlsx", data)
			require.ErrorIs(t, err, errExactWorkbookUnsupported)
		})
	}
}

func TestExactStandaloneThreadedCommentsRequireAuthorsAndAcyclicSameCellParents(t *testing.T) {
	const person = "11111111-1111-4111-8111-111111111111"
	const root = "22222222-2222-4222-8222-222222222222"
	const reply = "33333333-3333-4333-8333-333333333333"
	for _, fault := range []string{"valid", "unknown author", "missing parent", "cycle", "different cell"} {
		t.Run(fault, func(t *testing.T) {
			parts := map[string][]byte{
				"xl/persons/person.xml": []byte(`<personList xmlns="` + standaloneThreadNS +
					`"><person id="` + person + `" displayName="Source author"/></personList>`),
				"xl/worksheets/_rels/sheet1.xml.rels": []byte(`<Relationships xmlns="` +
					`http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="thread" Type="` +
					standaloneThreadRelation + `" Target="../threadedcomments/thread.xml"/></Relationships>`),
			}
			author, parent, rootParent, ref := person, root, "{}", "A1"
			switch fault {
			case "unknown author":
				author = reply
			case "missing parent":
				parent = person
			case "cycle":
				rootParent = reply
			case "different cell":
				ref = "B1"
			}
			parts["xl/threadedcomments/thread.xml"] = []byte(`<ThreadedComments xmlns="` + standaloneThreadNS +
				`"><threadedComment id="` + root + `" personId="` + person + `" parentId="` + rootParent +
				`" ref="A1"><text>Original annotation</text></threadedComment><threadedComment id="` + reply +
				`" personId="` + author + `" parentId="` + parent + `" ref="` + ref +
				`"><text>Source reply</text></threadedComment></ThreadedComments>`)
			notes, err := standaloneThreadFacts(parts, []standaloneWorkbookSheet{{Part: "xl/worksheets/sheet1.xml"}})
			if fault == "valid" {
				require.NoError(t, err)
				require.Contains(t, notes["xl/worksheets/sheet1.xml"], "Source reply")
				require.Contains(t, notes["xl/worksheets/sheet1.xml"], "Source author")
			} else {
				require.ErrorIs(t, err, errExactWorkbookUnsupported)
			}
		})
	}
}

func TestExactStandaloneWorkbookHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := parseExactStandaloneWorkbook(ctx, "source.xlsx", standaloneTestWorkbook(t, nil))
	require.ErrorIs(t, err, context.Canceled)
}

func TestExactStandaloneImagesRequireActualOwnerReferences(t *testing.T) {
	for _, fault := range []string{"valid", "unused alias", "missing reference", "wrong relationship type"} {
		t.Run(fault, func(t *testing.T) {
			owner := "xl/worksheets/sheet1.xml"
			relationType := "image"
			if fault == "wrong relationship type" {
				relationType = "drawing"
			}
			parts := map[string][]byte{
				owner: []byte(`<worksheet xmlns="` + standaloneMainNS + `" xmlns:r="` +
					exactWorkbookRelNamespace + `"><picture r:id="picture"/></worksheet>`),
				"xl/worksheets/_rels/sheet1.xml.rels": []byte(`<Relationships xmlns="` +
					`http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="picture" Type="` +
					exactWorkbookRelNamespace + `/` + relationType + `" Target="../media/image.png"/></Relationships>`),
			}
			if fault == "unused alias" {
				appendStandaloneTestXML(parts, "xl/worksheets/_rels/sheet1.xml.rels", "</Relationships>",
					`<Relationship Id="alias" Type="`+exactWorkbookRelNamespace+`/image" Target="../media/image.png"/>`)
			}
			if fault == "missing reference" {
				parts["xl/worksheets/_rels/sheet1.xml.rels"] = nil
			}
			err := standaloneImageReferences(parts, owner, "picture", "drawing")
			if fault == "valid" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, errExactWorkbookUnsupported)
			}
		})
	}
}
