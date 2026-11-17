package service

import (
	"fmt"
	"path"
	"strings"
)

const standaloneChartNS = "http://schemas.openxmlformats.org/drawingml/2006/chart"

// Preserve drawing placement, titles and chart caches as literal source facts.
// No renderer, formula evaluator or external relationship resolver is invoked.
func standaloneWorkbookDrawingFacts(
	parts map[string][]byte, sheets []standaloneWorkbookSheet,
) (map[string]string, error) {
	notes := map[string]string{}
	used := map[string]bool{}
	for _, sheet := range sheets {
		rels, err := standaloneWorkbookRelations(parts, sheet.Part)
		if err != nil {
			return nil, err
		}
		for _, rel := range rels {
			part := standaloneRelationshipTarget(sheet.Part, rel)
			if rel.Type == exactWorkbookRelNamespace+"/image" {
				used[part] = true
			}
			if rel.Type != exactWorkbookRelNamespace+"/drawing" {
				continue
			}
			if used[part] {
				return nil, errExactWorkbookUnsupported
			}
			used[part] = true
			facts, err := standaloneXMLFacts(parts[part], map[string]string{
				exactDrawingNamespace: "wsDr twoCellAnchor oneCellAnchor absoluteAnchor from to col colOff " +
					"row rowOff pos ext pic nvPicPr cNvPr cNvPicPr blipFill spPr clientData " +
					"graphicFrame nvGraphicFramePr cNvGraphicFramePr xfrm",
				exactDrawingMainNamespace: "picLocks blip stretch fillRect xfrm off ext prstGeom avLst " +
					"graphic graphicData graphicFrameLocks",
				standaloneChartNS: "chart",
			}, false)
			if err != nil {
				return nil, fmt.Errorf("drawing_facts: %w", err)
			}
			if err := standaloneImageReferences(parts, part, "blip", ""); err != nil {
				return nil, err
			}
			notes[sheet.Part] += "\nWorkbook drawing " + part + ":\n" + facts
			children, err := standaloneWorkbookRelations(parts, part)
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				target := standaloneRelationshipTarget(part, child)
				used[target] = true
				if child.Type == exactWorkbookRelNamespace+"/image" {
					continue
				}
				if child.Type != exactWorkbookRelNamespace+"/chart" {
					return nil, errExactWorkbookUnsupported
				}
				facts, err := standaloneChartFacts(parts[target])
				if err != nil {
					return nil, err
				}
				notes[sheet.Part] += "\nWorkbook chart " + target + ":\n" + facts
			}
		}
	}
	for part := range parts {
		owner := part
		if strings.HasPrefix(part, "xl/drawings/_rels/") {
			owner = "xl/drawings/" + strings.TrimSuffix(path.Base(part), ".rels")
		}
		if (strings.HasPrefix(part, "xl/media/") || strings.HasPrefix(part, "xl/charts/") ||
			strings.HasPrefix(part, "xl/drawings/") && !strings.HasSuffix(part, ".vml")) && !used[owner] {
			return nil, errExactWorkbookUnsupported
		}
	}
	return notes, nil
}

func standaloneChartFacts(body []byte) (string, error) {
	return standaloneXMLFacts(body, map[string]string{
		standaloneChartNS: "chartSpace date1904 lang roundedCorners chart title tx rich layout manualLayout " +
			"xMode yMode wMode hMode layoutTarget x y w h overlay spPr txPr plotArea " +
			"pieChart pie3DChart doughnutChart barChart lineChart areaChart scatterChart " +
			"radarChart bubbleChart grouping barDir varyColors ser idx order " +
			"cat val xVal yVal bubbleSize strRef numRef multiLvlStrRef strCache numCache " +
			"multiLvlStrCache strLit numLit f formatCode ptCount pt v lvl dPt marker " +
			"symbol size smooth explosion " +
			"dLbls dLbl showLegendKey showVal showCatName showSerName showPercent showBubbleSize separator " +
			"dLblPos firstSliceAng holeSize gapWidth overlap axId catAx valAx dateAx scaling logBase orientation " +
			"max min delete axPos majorGridlines minorGridlines numFmt majorTickMark minorTickMark tickLblPos " +
			"crossAx crosses crossesAt auto lblAlgn lblOffset noMultiLvlLbl crossBetween majorUnit minorUnit " +
			"legend legendPos legendEntry plotVisOnly dispBlanksAs showDLblsOverMax printSettings pageMargins " +
			"headerFooter pageSetup style dispUnits builtInUnit dispUnitsLbl",
		exactDrawingMainNamespace: "bodyPr lstStyle p pPr defRPr r rPr t endParaRPr solidFill schemeClr srgbClr " +
			"sysClr lumMod lumOff alpha latin ea cs noFill ln prstDash round bevel miter",
	}, false)
}
