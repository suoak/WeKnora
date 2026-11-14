package service

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Read the saved value and formula as separate source facts. GetCellFormula
// expands shared references; neither it nor GetCellValue evaluates the formula.
func exactWorkbookFormulaNotes(file *excelize.File, sheet string, body []byte) (string, error) {
	return exactWorkbookFormulaNotesForFormat(file, sheet, body, false)
}

func exactWorkbookFormulaNotesForFormat(
	file *excelize.File, sheet string, body []byte, standalone bool,
) (string, error) {
	type formula struct {
		Text  string     `xml:",chardata"`
		Attrs []xml.Attr `xml:",any,attr"`
	}
	type cell struct {
		Ref     string   `xml:"r,attr"`
		Formula *formula `xml:"f"`
	}
	var ws struct {
		Rows []struct {
			Cells []cell `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if xml.Unmarshal(body, &ws) != nil {
		return "", errExactWorkbookUnsupported
	}
	groups := map[string]string{}
	used := map[string]map[string]bool{}
	formulas := map[string]*formula{}
	for _, row := range ws.Rows {
		for _, c := range row.Cells {
			if c.Formula == nil {
				continue
			}
			if strings.ContainsAny(c.Formula.Text, "[|\x00") || len(c.Formula.Text) > 8192 {
				return "", errExactWorkbookUnsupported
			}
			if formulas[c.Ref] != nil || len(formulas) >= 1024 {
				return "", errExactWorkbookUnsupported
			}
			formulas[c.Ref] = c.Formula
			a := map[string]string{}
			for _, attr := range c.Formula.Attrs {
				if attr.Name.Space != "" ||
					(attr.Name.Local != "t" && attr.Name.Local != "si" && attr.Name.Local != "ref" &&
						(!standalone || (attr.Name.Local != "aca" && attr.Name.Local != "ca"))) {
					return "", errExactWorkbookUnsupported
				}
				if _, exists := a[attr.Name.Local]; exists {
					return "", errExactWorkbookUnsupported
				}
				a[attr.Name.Local] = attr.Value
			}
			if standalone && a["t"] == "array" {
				if a["si"] != "" || strings.TrimSpace(c.Formula.Text) == "" ||
					!standaloneArrayRange(a["ref"], c.Ref) {
					return "", errExactWorkbookUnsupported
				}
				continue
			}
			if a["t"] != "" && a["t"] != "normal" && a["t"] != "shared" {
				return "", errExactWorkbookUnsupported
			}
			if a["t"] != "shared" {
				if a["si"] != "" || a["ref"] != "" || (!standalone && strings.TrimSpace(c.Formula.Text) == "") {
					return "", errExactWorkbookUnsupported
				}
				continue
			}
			if a["si"] == "" {
				return "", errExactWorkbookUnsupported
			}
			if used[a["si"]] == nil {
				used[a["si"]] = map[string]bool{}
			}
			used[a["si"]][c.Ref] = true
			if c.Formula.Text != "" {
				if groups[a["si"]] != "" || a["ref"] == "" {
					return "", errExactWorkbookUnsupported
				}
				groups[a["si"]] = a["ref"]
			} else if a["ref"] != "" {
				return "", errExactWorkbookUnsupported
			}
		}
	}
	for group, refs := range used {
		ends := strings.Split(groups[group], ":")
		if len(ends) != 2 {
			return "", errExactWorkbookUnsupported
		}
		left, top, e := excelize.CellNameToCoordinates(ends[0])
		right, bottom, e2 := excelize.CellNameToCoordinates(ends[1])
		if e != nil || e2 != nil || left > right || top > bottom || right > 64 || bottom > 1024 ||
			(right-left+1)*(bottom-top+1) != len(refs) {
			return "", errExactWorkbookUnsupported
		}
		for row := top; row <= bottom; row++ {
			for col := left; col <= right; col++ {
				ref, _ := excelize.CoordinatesToCellName(col, row)
				if !refs[ref] {
					return "", errExactWorkbookUnsupported
				}
			}
		}
	}
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#13;", "\n", "&#10;", "|", "&#124;")
	var notes strings.Builder
	for _, row := range ws.Rows {
		for _, c := range row.Cells {
			if c.Formula == nil {
				continue
			}
			text, err := file.GetCellFormula(sheet, c.Ref)
			if err != nil || (!standalone && strings.TrimSpace(text) == "") || len(text) > 8192 {
				return "", errExactWorkbookUnsupported
			}
			// External workbook/DDE inputs cannot be represented by this package.
			if strings.ContainsAny(text, "[|\x00") {
				return "", errExactWorkbookUnsupported
			}
			if strings.TrimSpace(text) == "" {
				text = "(empty saved formula)"
			}
			fmt.Fprintf(&notes, "\nFormula cell %s (saved value shown above; formula not recalculated): %s\n",
				c.Ref, escape.Replace(text))
		}
	}
	return notes.String(), nil
}
