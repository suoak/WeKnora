package service

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestExactWorkbookHyperlinkNativeCoverage(t *testing.T) {
	if !anydoc.Available() {
		t.Skip("requires linked native anydoc parser")
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	f := excelize.NewFile()
	defer f.Close()
	require.NoError(t, f.SetCellStr("Sheet1", "A1", "original label"))
	require.NoError(t, f.SetCellHyperLink("Sheet1", "A1", server.URL+"/never-fetch", "External"))
	require.NoError(t, f.SetCellFormula("Sheet1", "B1", `HYPERLINK("`+server.URL+`/formula-never-fetch","formula label")`))
	b, err := f.WriteToBuffer()
	require.NoError(t, err)
	book, err := parseExactEmbeddedWorkbook("word/embeddings/test.xlsx", b.Bytes(), 1)
	require.NoError(t, err)
	require.Len(t, book.Sections, 1)
	require.Contains(t, book.Sections[0].markdown, "original label")
	require.Contains(t, book.Sections[0].markdown, server.URL+"/never-fetch")
	require.Contains(t, book.Sections[0].markdown, "formula label")
	require.EqualValues(t, 0, requests.Load(), "native conversion must not fetch literal link targets")
}

func TestExactWorkbookParentExpansionBudget(t *testing.T) {
	var buffer bytes.Buffer
	w := zip.NewWriter(&buffer)
	e, err := w.Create("xl/worksheets/sheet1.xml")
	require.NoError(t, err)
	_, err = e.Write(bytes.Repeat([]byte("x"), 8<<20))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	packages := map[string][]byte{}
	for i := 0; i < 8; i++ {
		packages[fmt.Sprint(i)] = buffer.Bytes()
	}
	require.NoError(t, checkExactWorkbookExpansionBudget(packages))
	packages["ninth"] = buffer.Bytes()
	require.ErrorIs(t, checkExactWorkbookExpansionBudget(packages), errExactWorkbookUnsupported)
}

func TestExactWorkbookLinksPreserveLiteralMetadata(t *testing.T) {
	f := excelize.NewFile()
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	require.NoError(t, f.SetCellStr("Sheet1", "A1", "source label"))
	display, tooltip := "source display", "source tooltip"
	require.NoError(t, f.SetCellHyperLink("Sheet1", "A1", "https://example.invalid/research?x=1&y=2", "External",
		excelize.HyperlinkOpts{Display: &display, Tooltip: &tooltip}))
	b, err := f.WriteToBuffer()
	require.NoError(t, err)
	parts, err := exactWorkbookParts(b.Bytes())
	require.NoError(t, err)
	notes, err := exactWorkbookHyperlinkNotes(parts)
	require.NoError(t, err)
	note := notes["xl/worksheets/sheet1.xml"]
	require.Contains(t, note, "Hyperlink cell A1")
	require.Contains(t, note, "https://example.invalid/research?x=1&amp;y=2")
	require.Contains(t, note, "display=source display")
	require.Contains(t, note, "tooltip=source tooltip")
	value, err := f.GetCellValue("Sheet1", "A1")
	require.NoError(t, err)
	require.Equal(t, "source label", value)
}

func TestExactWorkbookLinksRejectUnprovedDependencies(t *testing.T) {
	for _, target := range []string{"file:///etc/passwd", "javascript:alert(1)", "ftp://example.invalid/a",
		"https://user:password@example.invalid/a"} {
		t.Run(target, func(t *testing.T) {
			f := excelize.NewFile()
			defer f.Close()
			require.NoError(t, f.SetCellStr("Sheet1", "A1", "label"))
			require.NoError(t, f.SetCellHyperLink("Sheet1", "A1", target, "External"))
			b, err := f.WriteToBuffer()
			require.NoError(t, err)
			_, err = exactWorkbookParts(b.Bytes())
			require.Error(t, err)
		})
	}
	for _, child := range []string{
		`<hyperlink ref="A2" r:id="link"/>`,
		`<drawing r:id="link"/>`,
		`<hyperlink ref="A1:B1" r:id="link"/>`,
		`<hyperlink ref="A1" r:id="missing"/>`,
		`<hyperlink ref="A1" r:id="link"/><hyperlink ref="A1" r:id="link"/>`,
		``,
	} {
		parts := map[string][]byte{
			"xl/worksheets/sheet1.xml":            []byte(`<worksheet xmlns:r="` + exactWorkbookRelNamespace + `"><sheetData><row><c r="A1"/></row></sheetData>` + child + `</worksheet>`),
			"xl/worksheets/_rels/sheet1.xml.rels": []byte(`<Relationships><Relationship Id="link" Type="` + exactWorkbookHyperlinkType + `" Target="https://example.invalid/" TargetMode="External"/></Relationships>`),
		}
		_, err := exactWorkbookHyperlinkNotes(parts)
		require.Error(t, err, child)
	}
}
