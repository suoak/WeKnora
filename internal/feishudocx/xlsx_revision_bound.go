package feishudocx

import (
	"encoding/xml"
	"io"
	"regexp"
)

const sheetDrawingNS = "http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing"
const sheetRevisionNS = "http://schemas.microsoft.com/office/spreadsheetml/2014/revision"

var (
	sheetDrawingPart = regexp.MustCompile(`^xl/drawings/drawing[0-9]+\.xml$`)
	sheetCommentPart = regexp.MustCompile(`^xl/comments[0-9]+\.xml$`)
	sheetCommentUID  = regexp.MustCompile(`^\{[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}\}$`)
)

func generatedSheetPictureDescription(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, ch := range []byte(value) {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') {
			return false
		}
	}
	return true
}

func revisionBoundXLSXAttrs(name xml.Name, attrs []xml.Attr, role string) ([]xml.Attr, error) {
	for i, attr := range attrs {
		if role == "drawing" && name == (xml.Name{Space: sheetDrawingNS, Local: "cNvPr"}) &&
			attr.Name == (xml.Name{Local: "descr"}) && generatedSheetPictureDescription(attr.Value) {
			// Same immutable revision is checked outside this package. An author
			// changing even six-letter alt text changes that revision and is refused.
			attrs[i].Value = "feishu-export-picture-description"
		}
		if role == "comments" && name == (xml.Name{Space: spreadsheetNS, Local: "comment"}) &&
			attr.Name == (xml.Name{Space: sheetRevisionNS, Local: "uid"}) {
			if !sheetCommentUID.MatchString(attr.Value) {
				return nil, ErrInvalidXLSXPackage
			}
			attrs[i].Value = "{00000000-0000-0000-0000-000000000000}"
		}
	}
	return attrs, nil
}

func streamRevisionBoundXLSXXML(r io.Reader, w io.Writer, role string) error {
	required := xml.Name{Space: sheetDrawingNS, Local: "wsDr"}
	if role == "comments" {
		required = xml.Name{Space: spreadsheetNS, Local: "comments"}
	}
	s := &xlsxXMLStream{d: xml.NewDecoder(r), w: w, snapshot: true, nonceRole: role}
	return s.document(required)
}
