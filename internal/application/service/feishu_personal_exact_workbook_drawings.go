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

const exactDrawingNamespace = "http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing"
const exactDrawingMainNamespace = "http://schemas.openxmlformats.org/drawingml/2006/main"

// Admit only embedded pictures with explicit two-cell placement. Charts,
// text boxes, linked media and unknown shapes still require their own reader.
func exactWorkbookDrawingNotes(parts map[string][]byte) (map[string]string, error) {
	notes := map[string]string{}
	usedDrawings, usedImages := map[string]bool{}, map[string]bool{}
	for sheet, body := range parts {
		if !strings.HasPrefix(sheet, "xl/worksheets/") ||
			strings.Contains(sheet, "/_rels/") || !strings.HasSuffix(sheet, ".xml") {
			continue
		}
		var ws struct {
			Pictures []struct {
				ID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
			} `xml:"picture"`
			Drawings []struct {
				ID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
			} `xml:"drawing"`
		}
		if xml.Unmarshal(body, &ws) != nil {
			return nil, errExactWorkbookUnsupported
		}
		if len(ws.Pictures) > 1 {
			return nil, errExactWorkbookUnsupported
		}
		for _, picture := range ws.Pictures {
			image, err := exactWorkbookRelatedPart(parts, sheet, picture.ID, "image")
			if err != nil || !strings.HasPrefix(image, "xl/media/") {
				return nil, errExactWorkbookUnsupported
			}
			usedImages[image] = true
		}
		for _, drawing := range ws.Drawings {
			drawingPart, err := exactWorkbookRelatedPart(parts, sheet, drawing.ID, "drawing")
			if err != nil || usedDrawings[drawingPart] || !strings.HasPrefix(drawingPart, "xl/drawings/") {
				return nil, errExactWorkbookUnsupported
			}
			usedDrawings[drawingPart] = true
			d := xml.NewDecoder(bytes.NewReader(parts[drawingPart]))
			allowed := map[string]string{
				"wsDr":          exactDrawingNamespace,
				"twoCellAnchor": exactDrawingNamespace,
				"from":          exactDrawingNamespace,
				"to":            exactDrawingNamespace,
				"col":           exactDrawingNamespace,
				"colOff":        exactDrawingNamespace,
				"row":           exactDrawingNamespace,
				"rowOff":        exactDrawingNamespace,
				"pic":           exactDrawingNamespace,
				"nvPicPr":       exactDrawingNamespace,
				"cNvPr":         exactDrawingNamespace,
				"cNvPicPr":      exactDrawingNamespace,
				"blipFill":      exactDrawingNamespace,
				"spPr":          exactDrawingNamespace,
				"clientData":    exactDrawingNamespace,
				"picLocks":      exactDrawingMainNamespace,
				"blip":          exactDrawingMainNamespace,
				"stretch":       exactDrawingMainNamespace,
				"fillRect":      exactDrawingMainNamespace,
				"xfrm":          exactDrawingMainNamespace,
				"off":           exactDrawingMainNamespace,
				"ext":           exactDrawingMainNamespace,
				"prstGeom":      exactDrawingMainNamespace,
				"avLst":         exactDrawingMainNamespace,
			}
			for {
				token, e := d.Token()
				if e == io.EOF {
					break
				}
				if e != nil {
					return nil, errExactWorkbookUnsupported
				}
				if _, ok := token.(xml.Directive); ok {
					return nil, errExactWorkbookUnsupported
				}
				if el, ok := token.(xml.StartElement); ok {
					if ns, exists := allowed[el.Name.Local]; !exists || ns != el.Name.Space {
						return nil, errExactWorkbookUnsupported
					}
					for _, a := range el.Attr {
						if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
							continue
						}
						if a.Name.Space == exactWorkbookRelNamespace && el.Name.Local == "blip" && a.Name.Local == "embed" {
							continue
						}
						permitted := map[string]string{
							"twoCellAnchor": "editAs",
							"cNvPr":         "id name descr title hidden",
							"picLocks":      "noChangeAspect",
							"off":           "x y",
							"ext":           "cx cy",
							"prstGeom":      "prst",
							"clientData":    "fLocksWithSheet fPrintsWithSheet",
						}
						if a.Name.Space != "" || !strings.Contains(" "+permitted[el.Name.Local]+" ", " "+a.Name.Local+" ") {
							return nil, errExactWorkbookUnsupported
						}
						if el.Name.Local == "prstGeom" && a.Value != "rect" {
							return nil, errExactWorkbookUnsupported
						}
					}
				}
			}
			type marker struct {
				Col    int   `xml:"col"`
				Row    int   `xml:"row"`
				ColOff int64 `xml:"colOff"`
				RowOff int64 `xml:"rowOff"`
			}
			var document struct {
				Anchors []struct {
					From     marker `xml:"from"`
					To       marker `xml:"to"`
					Pictures []struct {
						Properties struct {
							Name        string `xml:"name,attr"`
							Description string `xml:"descr,attr"`
							Title       string `xml:"title,attr"`
						} `xml:"nvPicPr>cNvPr"`
						Blips []struct {
							ID string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships embed,attr"`
						} `xml:"blipFill>blip"`
					} `xml:"pic"`
				} `xml:"twoCellAnchor"`
			}
			if xml.Unmarshal(parts[drawingPart], &document) != nil || len(document.Anchors) == 0 || len(document.Anchors) > 64 {
				return nil, errExactWorkbookUnsupported
			}
			seenRels := map[string]bool{}
			for _, anchor := range document.Anchors {
				if len(anchor.Pictures) != 1 || len(anchor.Pictures[0].Blips) != 1 {
					return nil, errExactWorkbookUnsupported
				}
				picture := anchor.Pictures[0]
				id := picture.Blips[0].ID
				image, err := exactWorkbookRelatedPart(parts, drawingPart, id, "image")
				if err != nil || !strings.HasPrefix(image, "xl/media/") {
					return nil, errExactWorkbookUnsupported
				}
				usedImages[image] = true
				seenRels[id] = true
				label := func(m marker) (string, error) {
					if m.Col < 0 || m.Col >= 64 || m.Row < 0 || m.Row >= 1024 ||
						m.ColOff < -(1<<30) || m.RowOff < -(1<<30) || m.ColOff > 1<<30 || m.RowOff > 1<<30 {
						return "", errExactWorkbookUnsupported
					}
					ref, _ := excelize.CoordinatesToCellName(m.Col+1, m.Row+1)
					return ref + " (offset " + strconv.FormatInt(m.ColOff, 10) + "," + strconv.FormatInt(m.RowOff, 10) + " EMU)", nil
				}
				from, e := label(anchor.From)
				to, e2 := label(anchor.To)
				if e != nil || e2 != nil {
					return nil, errExactWorkbookUnsupported
				}
				escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\n", "&#10;", "\r", "&#13;", "|", "&#124;")
				notes[sheet] += fmt.Sprintf("\nEmbedded picture %s: from %s to %s; name=%s; description=%s; title=%s.\n", path.Base(image), from, to, escape.Replace(picture.Properties.Name), escape.Replace(picture.Properties.Description), escape.Replace(picture.Properties.Title))
			}
			var rels struct {
				Rows []struct {
					ID string `xml:"Id,attr"`
				} `xml:"Relationship"`
			}
			if xml.Unmarshal(parts[path.Dir(drawingPart)+"/_rels/"+path.Base(drawingPart)+".rels"], &rels) != nil {
				return nil, errExactWorkbookUnsupported
			}
			for _, r := range rels.Rows {
				if !seenRels[r.ID] {
					return nil, errExactWorkbookUnsupported
				}
			}
		}
	}
	for name := range parts {
		if strings.HasPrefix(name, "xl/drawings/") {
			parent := name
			if strings.Contains(name, "/_rels/") {
				parent = "xl/drawings/" + strings.TrimSuffix(path.Base(name), ".rels")
			}
			if !usedDrawings[parent] {
				return nil, errExactWorkbookUnsupported
			}
		}
		if strings.HasPrefix(name, "xl/media/") && !usedImages[name] {
			return nil, errExactWorkbookUnsupported
		}
	}
	return notes, nil
}

func exactWorkbookRelatedPart(parts map[string][]byte, owner, id, kind string) (string, error) {
	var rels struct {
		Rows []struct {
			ID     string `xml:"Id,attr"`
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if id == "" || xml.Unmarshal(parts[path.Dir(owner)+"/_rels/"+path.Base(owner)+".rels"], &rels) != nil {
		return "", errExactWorkbookUnsupported
	}
	for _, r := range rels.Rows {
		if r.ID == id {
			if r.Type != exactWorkbookRelNamespace+"/"+kind || (r.Mode != "" && r.Mode != "Internal") {
				return "", errExactWorkbookUnsupported
			}
			resolved := strings.ToLower(path.Clean(path.Join(path.Dir(owner), r.Target)))
			if strings.HasPrefix(r.Target, "/") {
				resolved = strings.ToLower(strings.TrimPrefix(r.Target, "/"))
			}
			if parts[resolved] == nil {
				return "", errExactWorkbookUnsupported
			}
			return resolved, nil
		}
	}
	return "", errExactWorkbookUnsupported
}
