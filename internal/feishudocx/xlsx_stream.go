package feishudocx

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"sort"
	"strings"
)

// XLSX tables can contain millions of small cells. Stream the exact v1 JSON
// representation instead of retaining an XML tree and a second JSON copy.
// Only exporter column definitions require buffering; those are separately bounded.
type xlsxXMLStream struct {
	d            *xml.Decoder
	w            io.Writer
	tokens       int
	columnTokens int
	columnStart  int64
}

func (s *xlsxXMLStream) next() (xml.Token, error) {
	t, err := s.d.Token()
	if err != nil {
		return nil, err
	}
	s.tokens++
	if s.tokens > 16000000 {
		return nil, ErrInvalidXLSXPackage
	}
	if s.columnStart != 0 && s.d.InputOffset()-s.columnStart > 4<<20 {
		return nil, ErrInvalidXLSXPackage
	}
	return t, nil
}

func (s *xlsxXMLStream) write(v string) { _, _ = io.WriteString(s.w, v) }

func xlsxAttrs(t xml.StartElement) ([]xml.Attr, error) {
	a := append([]xml.Attr(nil), t.Attr...)
	seen := make(map[xml.Name]bool, len(a))
	for _, v := range a {
		if seen[v.Name] {
			return nil, ErrInvalidXLSXPackage
		}
		seen[v.Name] = true
	}
	sort.Slice(a, func(i, j int) bool {
		return a[i].Name.Space < a[j].Name.Space ||
			a[i].Name.Space == a[j].Name.Space && a[i].Name.Local < a[j].Name.Local
	})
	return a, nil
}

func (s *xlsxXMLStream) columns(t xml.StartElement, depth int) (*node, error) {
	if depth >= 128 {
		return nil, ErrInvalidXLSXPackage
	}
	a, err := xlsxAttrs(t)
	if err != nil {
		return nil, err
	}
	n := &node{Name: t.Name, Attrs: a}
	for {
		v, e := s.next()
		if e != nil {
			return nil, ErrInvalidXLSXPackage
		}
		s.columnTokens++
		if s.columnTokens > 100000 {
			return nil, ErrInvalidXLSXPackage
		}
		switch v := v.(type) {
		case xml.StartElement:
			child, e := s.columns(v, depth+1)
			if e != nil {
				return nil, e
			}
			n.Parts = append(n.Parts, piece{Kind: "element", Node: child})
		case xml.EndElement:
			return n, nil
		case xml.CharData:
			n.Parts = append(n.Parts, piece{Kind: "text", Value: string(v)})
		default:
			return nil, ErrInvalidXLSXPackage
		}
	}
}

func (s *xlsxXMLStream) element(t xml.StartElement, depth int) error {
	if depth >= 128 {
		return ErrInvalidXLSXPackage
	}
	a, err := xlsxAttrs(t)
	if err != nil {
		return err
	}
	nameJSON, _ := json.Marshal(t.Name)
	attrsJSON, _ := json.Marshal(a)
	s.write(`{"Name":` + string(nameJSON) + `,"Attrs":` + string(attrsJSON) + `,"Parts":`)
	parts, cols := 0, 0
	begin := func() {
		if parts == 0 {
			s.write("[")
		} else {
			s.write(",")
		}
		parts++
	}
	for {
		v, e := s.next()
		if e != nil {
			return ErrInvalidXLSXPackage
		}
		switch v := v.(type) {
		case xml.StartElement:
			begin()
			if t.Name == (xml.Name{Space: spreadsheetNS, Local: "worksheet"}) &&
				v.Name == (xml.Name{Space: spreadsheetNS, Local: "cols"}) {
				cols++
				if cols > 1 {
					return ErrInvalidXLSXPackage
				}
				start := s.d.InputOffset()
				s.columnStart = start
				n, e := s.columns(v, depth+1)
				s.columnStart = 0
				if e != nil || s.d.InputOffset()-start > 4<<20 {
					return ErrInvalidXLSXPackage
				}
				wrapper := &node{Parts: []piece{{Kind: "element", Node: n}}}
				if e := normalizeExportColumnWidths(wrapper); e != nil {
					return e
				}
				b, _ := json.Marshal(piece{Kind: "element", Node: n})
				s.write(string(b))
			} else {
				s.write(`{"Kind":"element","Node":`)
				if e := s.element(v, depth+1); e != nil {
					return e
				}
				s.write("}")
			}
		case xml.EndElement:
			if parts == 0 {
				s.write("null}")
			} else {
				s.write("]}")
			}
			return nil
		case xml.CharData:
			begin()
			b, _ := json.Marshal(piece{Kind: "text", Value: string(v)})
			s.write(string(b))
		case xml.Comment:
			begin()
			b, _ := json.Marshal(piece{Kind: "comment", Value: string(v)})
			s.write(string(b))
		default:
			return ErrInvalidXLSXPackage
		}
	}
}

func streamXLSXXML(r io.Reader, w io.Writer, required xml.Name) error {
	s := &xlsxXMLStream{d: xml.NewDecoder(r), w: w}
	root, declaration := false, false
	for {
		v, err := s.next()
		if err == io.EOF {
			if root {
				return nil
			}
			return ErrInvalidXLSXPackage
		}
		if err != nil {
			return ErrInvalidXLSXPackage
		}
		switch v := v.(type) {
		case xml.StartElement:
			if root || required.Local != "" && v.Name != required {
				return ErrInvalidXLSXPackage
			}
			root = true
			if e := s.element(v, 0); e != nil {
				return e
			}
		case xml.CharData:
			if strings.TrimSpace(string(v)) != "" {
				return ErrInvalidXLSXPackage
			}
		case xml.ProcInst:
			if root || declaration || v.Target != "xml" {
				return ErrInvalidXLSXPackage
			}
			declaration = true
		default:
			return ErrInvalidXLSXPackage
		}
	}
}
