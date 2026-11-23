package feishudocx

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var validationRange = regexp.MustCompile(
	`^\$?([A-Z]{1,3})\$?([1-9][0-9]{0,6})(?::\$?([A-Z]{1,3})\$?([1-9][0-9]{0,6}))?$`,
)

type validationRectangle struct{ left, right, top, bottom, rule int }

func validationCell(column, row string) (int, int, error) {
	x := 0
	for _, c := range column {
		x = x*26 + int(c-'A'+1)
	}
	y, err := strconv.Atoi(row)
	if err != nil || x < 1 || x > 16384 || y < 1 || y > 1048576 {
		return 0, 0, ErrInvalidXLSXPackage
	}
	return x, y, nil
}

// Disjoint validation rules have no priority relationship. Keep every rule,
// formula, attribute and range, and refuse overlap instead of discarding order.
func normalizeDisjointValidations(root *node) error {
	type ordered struct {
		part piece
		key  string
	}
	parts := []ordered{}
	ranges := []validationRectangle{}
	for _, part := range root.Parts {
		if part.Kind == "text" && strings.TrimSpace(part.Value) == "" {
			continue
		}
		n := part.Node
		if part.Kind != "element" || n == nil ||
			n.Name != (xml.Name{Space: spreadsheetNS, Local: "dataValidation"}) || len(parts) >= 512 {
			return ErrInvalidXLSXPackage
		}
		ref := ""
		for _, a := range n.Attrs {
			if a.Name == (xml.Name{Local: "sqref"}) {
				ref = a.Value
			}
		}
		if strings.TrimSpace(ref) == "" {
			return ErrInvalidXLSXPackage
		}
		for _, value := range strings.Fields(ref) {
			m := validationRange.FindStringSubmatch(strings.ToUpper(value))
			if m == nil || len(ranges) >= 1024 {
				return ErrInvalidXLSXPackage
			}
			left, top, err := validationCell(m[1], m[2])
			if err != nil {
				return err
			}
			right, bottom := left, top
			if m[3] != "" {
				right, bottom, err = validationCell(m[3], m[4])
				if err != nil {
					return err
				}
			}
			if left > right || top > bottom {
				return ErrInvalidXLSXPackage
			}
			r := validationRectangle{left, right, top, bottom, len(parts)}
			for _, old := range ranges {
				if old.rule != r.rule && r.left <= old.right && old.left <= r.right &&
					r.top <= old.bottom && old.top <= r.bottom {
					return ErrInvalidXLSXPackage
				}
			}
			ranges = append(ranges, r)
		}
		encoded, err := json.Marshal(part)
		if err != nil {
			return err
		}
		parts = append(parts, ordered{part, string(encoded)})
	}
	for _, attr := range root.Attrs {
		if attr.Name == (xml.Name{Local: "count"}) {
			count, err := strconv.Atoi(attr.Value)
			if err != nil || count != len(parts) {
				return ErrInvalidXLSXPackage
			}
		}
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].key < parts[j].key })
	root.Parts = make([]piece, len(parts))
	for i, p := range parts {
		root.Parts[i] = p.part
	}
	return nil
}

func streamOrderedRevisionXLSXXML(r io.Reader, w io.Writer, links map[string]string) error {
	s := &xlsxXMLStream{d: xml.NewDecoder(r), w: w, snapshot: true, links: links, orderedRules: true}
	return s.document(xml.Name{Space: spreadsheetNS, Local: "worksheet"})
}
