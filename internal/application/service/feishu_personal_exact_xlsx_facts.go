package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Supplementary source facts are literal text, never executable XML or formula
// inputs. Keep attributes and text, reject unknown element namespaces, and split
// on complete fact lines rather than dropping annotations or truncating values.
func standaloneXMLFacts(body []byte, schema map[string]string, worksheet bool) (string, error) {
	d := xml.NewDecoder(bytes.NewReader(body))
	var stack []string
	var out strings.Builder
	roots := 0
	cellChildren := map[string]bool{}
	sheetDataSeen := false
	for {
		token, err := d.Token()
		if err == io.EOF {
			if roots != 1 || len(stack) != 0 {
				return "", errExactWorkbookUnsupported
			}
			break
		}
		if err != nil {
			return "", errExactWorkbookUnsupported
		}
		emit := func() bool {
			if !worksheet {
				return true
			}
			for _, name := range stack {
				if name == "sheetData" {
					return stack[len(stack)-1] == "f"
				}
			}
			return len(stack) > 1 && !strings.Contains(
				" dimension sheetViews sheetView pane selection sheetFormatPr cols col pageMargins ",
				" "+stack[1]+" ")
		}
		switch token := token.(type) {
		case xml.Directive:
			return "", errExactWorkbookUnsupported
		case xml.StartElement:
			if worksheet && token.Name.Space == standaloneMainNS {
				parent := ""
				if len(stack) > 0 {
					parent = stack[len(stack)-1]
				}
				switch token.Name.Local {
				case "sheetData":
					if parent != "worksheet" || sheetDataSeen {
						return "", errExactWorkbookUnsupported
					}
					sheetDataSeen = true
				case "row":
					if parent != "sheetData" {
						return "", errExactWorkbookUnsupported
					}
				case "c":
					if parent != "row" {
						return "", errExactWorkbookUnsupported
					}
					cellChildren = map[string]bool{}
				case "f", "v", "is":
					if parent != "c" || cellChildren[token.Name.Local] {
						return "", errExactWorkbookUnsupported
					}
					cellChildren[token.Name.Local] = true
				}
			}
			if len(stack) == 0 {
				roots++
				if roots > 1 {
					return "", errExactWorkbookUnsupported
				}
			}
			if !strings.Contains(" "+schema[token.Name.Space]+" ", " "+token.Name.Local+" ") {
				return "", fmt.Errorf("standalone_xml_element_%s: %w", token.Name.Local, errExactWorkbookUnsupported)
			}
			stack = append(stack, token.Name.Local)
			if len(stack) > 32 {
				return "", errExactWorkbookUnsupported
			}
			var attributes []string
			seen := map[xml.Name]bool{}
			for _, attr := range token.Attr {
				if seen[attr.Name] || len(attr.Value) > 8192 {
					return "", errExactWorkbookUnsupported
				}
				seen[attr.Name] = true
				if attr.Name.Space == "xmlns" || attr.Name.Local == "xmlns" {
					continue
				}
				if attr.Name.Space == exactWorkbookRelNamespace &&
					(attr.Name.Local != "id" || !strings.Contains(" sheet drawing legacyDrawing picture hyperlink ",
						" "+token.Name.Local+" ")) {
					return "", errExactWorkbookUnsupported
				}
				if attr.Name.Space != "" && attr.Name.Space != exactWorkbookRelNamespace &&
					attr.Name.Space != "http://www.w3.org/XML/1998/namespace" &&
					attr.Name.Space != "http://schemas.openxmlformats.org/markup-compatibility/2006" &&
					!strings.HasPrefix(attr.Name.Space, "http://schemas.microsoft.com/office/spreadsheetml/") {
					return "", fmt.Errorf("standalone_xml_attribute_%s: %w",
						attr.Name.Local, errExactWorkbookUnsupported)
				}
				attributes = append(attributes, attr.Name.Local+"="+standaloneFactText(attr.Value))
			}
			if emit() {
				fmt.Fprintf(&out, "\nSource fact %s: %s\n", strings.Join(stack, "/"), strings.Join(attributes, "; "))
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 && strings.TrimSpace(string(token)) != "" {
				return "", errExactWorkbookUnsupported
			}
			if emit() && strings.TrimSpace(string(token)) != "" {
				value := string(token)
				last := stack[len(stack)-1]
				formula := last == "definedName" || last == "f" || last == "formula1" || last == "formula2"
				if len(value) > 16<<10 || formula && strings.ContainsAny(value, "[|\x00") {
					return "", errExactWorkbookUnsupported
				}
				fmt.Fprintf(&out, "Source fact %s text: %s\n", strings.Join(stack, "/"), standaloneFactText(value))
			}
		}
		if out.Len() > 1<<20 {
			return "", errExactWorkbookUnsupported
		}
	}
	return out.String(), nil
}

func standaloneFactText(value string) string {
	return strings.NewReplacer("|", "&#124;", "\r", "&#13;", "\n", "&#10;").Replace(html.EscapeString(value))
}

func standaloneWorkbookFacts(
	parts map[string][]byte, sheets []standaloneWorkbookSheet, file *excelize.File,
) (map[string]string, error) {
	notes := map[string]string{}
	bookSchema := map[string]string{
		standaloneMainNS: "workbook fileVersion workbookPr bookViews workbookView sheets sheet definedNames " +
			"definedName calcPr extLst ext",
		"http://schemas.microsoft.com/office/spreadsheetml/2010/11/main":      "workbookPr",
		"http://schemas.microsoft.com/office/spreadsheetml/2018/calcfeatures": "calcFeatures feature",
	}
	book, err := standaloneXMLFacts(parts["xl/workbook.xml"], bookSchema, false)
	if err != nil {
		return nil, fmt.Errorf("workbook_facts: %w", err)
	}
	notes[sheets[0].Part] = book
	threaded, err := standaloneThreadFacts(parts, sheets)
	if err != nil {
		return nil, fmt.Errorf("thread_facts: %w", err)
	}
	for sheet, text := range threaded {
		notes[sheet] += text
	}
	metadataCount, metadataFacts, err := standaloneDynamicMetadata(parts["xl/metadata.xml"])
	if err != nil {
		return nil, fmt.Errorf("dynamic_metadata: %w", err)
	}
	notes[sheets[0].Part] += metadataFacts
	worksheetSchema := map[string]string{standaloneMainNS: "worksheet sheetPr tabColor outlinePr pageSetUpPr " +
		"dimension sheetViews sheetView pane selection sheetFormatPr cols col sheetData row c f v is t r rPr " +
		"rFont b i u strike color sz family charset scheme vertAlign mergeCells mergeCell phoneticPr " +
		"printOptions pageMargins pageSetup headerFooter oddHeader oddFooter evenHeader evenFooter firstHeader " +
		"firstFooter drawing legacyDrawing picture dataValidations dataValidation formula1 formula2 autoFilter " +
		"filterColumn filters filter customFilters customFilter sortState sortCondition sheetProtection " +
		"protectedRanges protectedRange hyperlinks hyperlink ignoredErrors ignoredError extLst ext"}
	validationNS := "http://schemas.microsoft.com/office/spreadsheetml/2009/9/main"
	worksheetSchema[validationNS] = "dataValidations dataValidation formula1 formula2"
	worksheetSchema["http://schemas.microsoft.com/office/excel/2006/main"] = "f sqref"
	formulaCells := map[int]map[string]bool{}
	for _, sheet := range sheets {
		facts, err := standaloneXMLFacts(parts[sheet.Part], worksheetSchema, true)
		if err != nil {
			return nil, fmt.Errorf("worksheet_facts: %w", err)
		}
		notes[sheet.Part] += facts
		var worksheet struct {
			XMLName xml.Name `xml:"http://schemas.openxmlformats.org/spreadsheetml/2006/main worksheet"`
			Rows    []struct {
				Cells []struct {
					Ref      string  `xml:"r,attr"`
					Metadata *string `xml:"cm,attr"`
					Value    *string `xml:"v"`
					Formula  *string `xml:"f"`
				} `xml:"c"`
			} `xml:"sheetData>row"`
		}
		if xml.Unmarshal(parts[sheet.Part], &worksheet) != nil {
			return nil, errExactWorkbookUnsupported
		}
		formulaCells[sheet.ID] = map[string]bool{}
		for _, row := range worksheet.Rows {
			for _, cell := range row.Cells {
				if cell.Metadata != nil {
					index, err := strconv.Atoi(*cell.Metadata)
					if err != nil || index <= 0 || index > metadataCount {
						return nil, errExactWorkbookUnsupported
					}
					notes[sheet.Part] += fmt.Sprintf("\nCell %s: metadata record %d.\n", cell.Ref, index)
				}
				if cell.Value != nil {
					raw, err := file.GetCellValue(sheet.Name, cell.Ref, excelize.Options{RawCellValue: true})
					display, displayErr := file.GetCellValue(sheet.Name, cell.Ref)
					if err != nil || displayErr != nil {
						return nil, errExactWorkbookUnsupported
					}
					if display != raw {
						notes[sheet.Part] += fmt.Sprintf("\nCell %s: display=%s; saved raw value=%s.\n", cell.Ref,
							standaloneFactText(display), standaloneFactText(raw))
					}
				}
				if cell.Formula != nil {
					formulaCells[sheet.ID][cell.Ref] = true
					if cell.Value == nil {
						notes[sheet.Part] += "\nFormula cell " + cell.Ref + ": no saved value; not evaluated.\n"
					}
				}
			}
		}
	}
	chain, err := standaloneCalcChain(parts["xl/calcchain.xml"], formulaCells)
	if err != nil {
		return nil, fmt.Errorf("calculation_chain: %w", err)
	}
	notes[sheets[0].Part] += chain
	return notes, nil
}

func standaloneCalcChain(body []byte, formulas map[int]map[string]bool) (string, error) {
	if len(body) == 0 {
		return "", nil
	}
	facts, err := standaloneXMLFacts(body, map[string]string{standaloneMainNS: "calcChain c"}, false)
	if err != nil {
		return "", err
	}
	var chain struct {
		XMLName xml.Name `xml:"http://schemas.openxmlformats.org/spreadsheetml/2006/main calcChain"`
		Cells   []struct {
			Ref   string `xml:"r,attr"`
			Sheet int    `xml:"i,attr"`
		} `xml:"c"`
	}
	if xml.Unmarshal(body, &chain) != nil || len(chain.Cells) > 65536 {
		return "", errExactWorkbookUnsupported
	}
	sheet := 0
	for _, cell := range chain.Cells {
		if cell.Sheet != 0 {
			sheet = cell.Sheet
		}
		if !formulas[sheet][cell.Ref] {
			return "", errExactWorkbookUnsupported
		}
	}
	return facts, nil
}

// Use stable worksheet/relationship ordering for receipts and repeated parses.
func standaloneSortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
