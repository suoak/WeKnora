package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Legacy VML is admitted only as the placement of ordinary cell comments.
// Preserve their text and authors; controls, images and other VML stay closed.
func exactWorkbookCommentNotes(parts map[string][]byte) (map[string]string, error) {
	notes, used := map[string]string{}, map[string]bool{}
	for sheet, body := range parts {
		if !strings.HasPrefix(sheet, "xl/worksheets/") || strings.Contains(sheet, "/_rels/") ||
			!strings.HasSuffix(sheet, ".xml") {
			continue
		}
		var ws struct {
			Drawings []struct {
				ID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
			} `xml:"legacyDrawing"`
		}
		if xml.Unmarshal(body, &ws) != nil || len(ws.Drawings) > 1 {
			return nil, errExactWorkbookUnsupported
		}
		if len(ws.Drawings) == 0 {
			continue
		}
		vml, err := exactWorkbookRelatedPart(parts, sheet, ws.Drawings[0].ID, "vmlDrawing")
		if err != nil || !strings.HasPrefix(vml, "xl/drawings/") || !strings.HasSuffix(vml, ".vml") || used[vml] {
			return nil, errExactWorkbookUnsupported
		}
		var rels struct {
			Rows []struct {
				ID   string `xml:"Id,attr"`
				Type string `xml:"Type,attr"`
			} `xml:"Relationship"`
		}
		relName := path.Dir(sheet) + "/_rels/" + path.Base(sheet) + ".rels"
		if xml.Unmarshal(parts[relName], &rels) != nil {
			return nil, errExactWorkbookUnsupported
		}
		commentPart := ""
		for _, rel := range rels.Rows {
			if rel.Type != exactWorkbookRelNamespace+"/comments" {
				continue
			}
			if commentPart != "" {
				return nil, errExactWorkbookUnsupported
			}
			commentPart, err = exactWorkbookRelatedPart(parts, sheet, rel.ID, "comments")
			if err != nil {
				return nil, err
			}
		}
		if commentPart == "" || used[commentPart] {
			return nil, errExactWorkbookUnsupported
		}
		var comments struct {
			Authors []string `xml:"authors>author"`
			Rows    []struct {
				Ref    string `xml:"ref,attr"`
				Author string `xml:"authorId,attr"`
				Text   struct {
					XML string `xml:",innerxml"`
				} `xml:"text"`
			} `xml:"commentList>comment"`
		}
		if xml.Unmarshal(parts[commentPart], &comments) != nil || len(comments.Rows) == 0 ||
			len(comments.Rows) > 1024 || len(comments.Authors) > 1024 {
			return nil, errExactWorkbookUnsupported
		}
		refs := map[string]bool{}
		escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;",
			"\r", "&#13;", "\n", "&#10;", "|", "&#124;")
		for _, comment := range comments.Rows {
			col, row, err := excelize.CellNameToCoordinates(comment.Ref)
			canonical, nameErr := excelize.CoordinatesToCellName(col, row)
			author, authorErr := strconv.Atoi(comment.Author)
			if err != nil || nameErr != nil || canonical != comment.Ref || col > 64 || row > 1024 ||
				refs[comment.Ref] || authorErr != nil || author < 0 || author >= len(comments.Authors) {
				return nil, errExactWorkbookUnsupported
			}
			text, err := exactWorkbookCommentText(comment.Text.XML)
			if err != nil {
				return nil, err
			}
			refs[comment.Ref] = true
			notes[sheet] += fmt.Sprintf("\nComment cell %s: author=%s; text=%s\n", comment.Ref,
				escape.Replace(comments.Authors[author]), escape.Replace(text))
		}
		if err := exactWorkbookCommentPlacement(parts[vml], refs); err != nil {
			return nil, err
		}
		used[vml], used[commentPart] = true, true
	}
	for part := range parts {
		if (strings.HasSuffix(part, ".vml") || strings.HasPrefix(part, "xl/comments")) && !used[part] {
			return nil, errExactWorkbookUnsupported
		}
	}
	return notes, nil
}

func exactWorkbookCommentText(body string) (string, error) {
	d := xml.NewDecoder(strings.NewReader("<text>" + body + "</text>"))
	allowed := " text r rPr rFont b i strike outline shadow condense extend color sz u vertAlign family charset scheme "
	var text strings.Builder
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", errExactWorkbookUnsupported
		}
		switch t := token.(type) {
		case xml.Directive:
			return "", errExactWorkbookUnsupported
		case xml.StartElement:
			if t.Name.Local == "t" {
				var value string
				if d.DecodeElement(&value, &t) != nil {
					return "", errExactWorkbookUnsupported
				}
				text.WriteString(value)
			} else if !strings.Contains(allowed, " "+t.Name.Local+" ") {
				return "", errExactWorkbookUnsupported
			}
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				return "", errExactWorkbookUnsupported
			}
		}
	}
	return text.String(), nil
}

func exactWorkbookCommentPlacement(body []byte, refs map[string]bool) error {
	d := xml.NewDecoder(bytes.NewReader(body))
	allowed := " xml shapelayout idmap shapetype stroke path shape fill shadow textbox div " +
		"ClientData MoveWithCells SizeWithCells Anchor AutoFill Row Column Visible "
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errExactWorkbookUnsupported
		}
		if _, ok := token.(xml.Directive); ok {
			return errExactWorkbookUnsupported
		}
		if el, ok := token.(xml.StartElement); ok {
			if !strings.Contains(allowed, " "+el.Name.Local+" ") {
				return errExactWorkbookUnsupported
			}
			for _, a := range el.Attr {
				v := strings.ToLower(a.Value)
				if a.Name.Local == "href" || a.Name.Local == "src" || a.Name.Local == "relid" ||
					strings.Contains(v, "url(") || strings.Contains(v, "javascript:") {
					return errExactWorkbookUnsupported
				}
			}
		}
	}
	var vml struct {
		Shapes []struct {
			Clients []struct {
				Type string `xml:"ObjectType,attr"`
				Row  *int   `xml:"Row"`
				Col  *int   `xml:"Column"`
			} `xml:"ClientData"`
		} `xml:"shape"`
	}
	if xml.Unmarshal(body, &vml) != nil || len(vml.Shapes) != len(refs) {
		return errExactWorkbookUnsupported
	}
	seen := map[string]bool{}
	for _, shape := range vml.Shapes {
		if len(shape.Clients) != 1 {
			return errExactWorkbookUnsupported
		}
		c := shape.Clients[0]
		if c.Type != "Note" || c.Row == nil || c.Col == nil {
			return errExactWorkbookUnsupported
		}
		ref, err := excelize.CoordinatesToCellName(*c.Col+1, *c.Row+1)
		if err != nil || !refs[ref] || seen[ref] {
			return errExactWorkbookUnsupported
		}
		seen[ref] = true
	}
	return nil
}
