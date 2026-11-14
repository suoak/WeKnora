package service

import (
	"bytes"
	"encoding/xml"
	"io"
	"path"
	"strings"
)

const (
	standaloneThreadRelation = "http://schemas.microsoft.com/office/2017/10/relationships/threadedComment"
	standalonePersonRelation = "http://schemas.microsoft.com/office/2017/10/relationships/person"
	standaloneThreadNS       = "http://schemas.microsoft.com/office/spreadsheetml/2018/threadedcomments"
	standaloneMainNS         = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
)

func validateStandaloneWorkbookGraph(parts map[string][]byte) error {
	if err := standaloneContentTypes(parts); err != nil {
		return err
	}
	seen := map[string]bool{"[content_types].xml": true}
	queue := []string{""}
	for len(queue) > 0 {
		owner := queue[0]
		queue = queue[1:]
		relName := path.Dir(owner) + "/_rels/" + path.Base(owner) + ".rels"
		if owner == "" {
			relName = "_rels/.rels"
		}
		if body := parts[relName]; body != nil {
			if err := standaloneRelationshipXML(body); err != nil {
				return err
			}
			seen[relName] = true
		}
		rels, err := standaloneWorkbookRelations(parts, owner)
		if err != nil {
			return err
		}
		roles := map[string]bool{}
		for _, rel := range rels {
			if owner == "" || owner == "xl/workbook.xml" && rel.Type != exactWorkbookRelNamespace+"/worksheet" {
				if roles[rel.Type] {
					return errExactWorkbookUnsupported
				}
				roles[rel.Type] = true
			}
			if rel.Type == exactWorkbookHyperlinkType && rel.Mode == "External" {
				if path.Dir(owner) != "xl/worksheets" {
					return errExactWorkbookUnsupported
				}
				continue // Literal URL only. No resolver or formula evaluator is invoked.
			}
			if !standaloneRelationAllowed(owner, rel.Type) ||
				(rel.Mode != "" && rel.Mode != "Internal") {
				return errExactWorkbookUnsupported
			}
			target := standaloneRelationshipTarget(owner, rel)
			if parts[target] == nil || !standaloneRelationTargetAllowed(owner, rel.Type, target) {
				return errExactWorkbookUnsupported
			}
			if !seen[target] {
				seen[target] = true
				queue = append(queue, target)
			}
		}
		if owner == "" && !roles[exactWorkbookRelNamespace+"/officeDocument"] {
			return errExactWorkbookUnsupported
		}
	}
	for name := range parts {
		if !seen[name] {
			return errExactWorkbookUnsupported
		}
	}
	return nil
}

func standaloneRelationTargetAllowed(owner, kind, target string) bool {
	switch kind {
	case exactWorkbookRelNamespace + "/officeDocument":
		return owner == "" && target == "xl/workbook.xml"
	case exactWorkbookRelNamespace + "/extended-properties":
		return owner == "" && target == "docprops/app.xml"
	case "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties":
		return owner == "" && target == "docprops/core.xml"
	case exactWorkbookRelNamespace + "/styles":
		return target == "xl/styles.xml"
	case exactWorkbookRelNamespace + "/sharedStrings":
		return target == "xl/sharedstrings.xml"
	case exactWorkbookRelNamespace + "/calcChain":
		return target == "xl/calcchain.xml"
	case exactWorkbookRelNamespace + "/sheetMetadata":
		return target == "xl/metadata.xml"
	case exactWorkbookRelNamespace + "/theme":
		return path.Dir(target) == "xl/theme" && strings.HasSuffix(target, ".xml")
	case exactWorkbookRelNamespace + "/worksheet":
		return path.Dir(target) == "xl/worksheets" && strings.HasSuffix(target, ".xml")
	case standalonePersonRelation:
		return path.Dir(target) == "xl/persons" && strings.HasSuffix(target, ".xml")
	case standaloneThreadRelation:
		return path.Dir(target) == "xl/threadedcomments" && strings.HasSuffix(target, ".xml")
	case exactWorkbookRelNamespace + "/image":
		return path.Dir(target) == "xl/media"
	case exactWorkbookRelNamespace + "/drawing":
		return path.Dir(target) == "xl/drawings" && strings.HasSuffix(target, ".xml")
	case exactWorkbookRelNamespace + "/vmlDrawing":
		return path.Dir(target) == "xl/drawings" && strings.HasSuffix(target, ".vml")
	case exactWorkbookRelNamespace + "/comments":
		return path.Dir(target) == "xl" && strings.HasPrefix(path.Base(target), "comments") &&
			strings.HasSuffix(target, ".xml")
	}
	return false
}

func standaloneRelationAllowed(owner, kind string) bool {
	if owner == "" {
		return kind == exactWorkbookRelNamespace+"/officeDocument" ||
			kind == exactWorkbookRelNamespace+"/extended-properties" ||
			kind == "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties"
	}
	if owner == "xl/workbook.xml" {
		return kind == standalonePersonRelation || kind == exactWorkbookRelNamespace+"/worksheet" ||
			kind == exactWorkbookRelNamespace+"/styles" || kind == exactWorkbookRelNamespace+"/theme" ||
			kind == exactWorkbookRelNamespace+"/sharedStrings" || kind == exactWorkbookRelNamespace+"/calcChain" ||
			kind == exactWorkbookRelNamespace+"/sheetMetadata"
	}
	if path.Dir(owner) == "xl/worksheets" {
		return kind == standaloneThreadRelation || kind == exactWorkbookRelNamespace+"/image" ||
			kind == exactWorkbookRelNamespace+"/drawing" || kind == exactWorkbookRelNamespace+"/vmlDrawing" ||
			kind == exactWorkbookRelNamespace+"/comments"
	}
	return path.Dir(owner) == "xl/drawings" && kind == exactWorkbookRelNamespace+"/image"
}

func standaloneRelationshipXML(body []byte) error {
	d := xml.NewDecoder(bytes.NewReader(body))
	depth, roots := 0, 0
	ids := map[string]bool{}
	for {
		token, err := d.Token()
		if err == io.EOF {
			if roots != 1 || depth != 0 {
				return errExactWorkbookUnsupported
			}
			return nil
		}
		if err != nil {
			return errExactWorkbookUnsupported
		}
		switch node := token.(type) {
		case xml.Directive:
			return errExactWorkbookUnsupported
		case xml.EndElement:
			depth--
		case xml.CharData:
			if strings.TrimSpace(string(node)) != "" {
				return errExactWorkbookUnsupported
			}
		case xml.StartElement:
			if depth == 0 {
				roots++
			}
			if roots != 1 || (depth == 0 && node.Name.Local != "Relationships") ||
				(depth == 1 && node.Name.Local != "Relationship") || depth > 1 {
				return errExactWorkbookUnsupported
			}
			depth++
			if node.Name.Space != "http://schemas.openxmlformats.org/package/2006/relationships" ||
				(node.Name.Local != "Relationships" && node.Name.Local != "Relationship") {
				return errExactWorkbookUnsupported
			}
			attributes := map[xml.Name]bool{}
			id, kind, target := "", "", ""
			for _, attr := range node.Attr {
				if attributes[attr.Name] {
					return errExactWorkbookUnsupported
				}
				attributes[attr.Name] = true
				if attr.Name.Space == "xmlns" || attr.Name.Local == "xmlns" {
					continue
				}
				if attr.Name.Space != "" || node.Name.Local != "Relationship" ||
					!strings.Contains(" Id Type Target TargetMode ", " "+attr.Name.Local+" ") {
					return errExactWorkbookUnsupported
				}
				switch attr.Name.Local {
				case "Id":
					id = attr.Value
				case "Type":
					kind = attr.Value
				case "Target":
					target = attr.Value
				}
			}
			if node.Name.Local == "Relationship" {
				if id == "" || kind == "" || target == "" || ids[id] {
					return errExactWorkbookUnsupported
				}
				ids[id] = true
			}
		}
	}
}

func standaloneContentTypes(parts map[string][]byte) error {
	var types struct {
		XMLName xml.Name `xml:"http://schemas.openxmlformats.org/package/2006/content-types Types"`
		Rows    []struct {
			XMLName   xml.Name
			Part      string `xml:"PartName,attr"`
			Extension string `xml:"Extension,attr"`
			Type      string `xml:"ContentType,attr"`
		} `xml:",any"`
	}
	if xml.Unmarshal(parts["[content_types].xml"], &types) != nil {
		return errExactWorkbookUnsupported
	}
	defaults, overrides := map[string]string{}, map[string]string{}
	allowed := map[string]bool{}
	for _, kind := range strings.Fields("application/xml image/png image/jpeg image/gif image/tiff " +
		"application/vnd.openxmlformats-package.relationships+xml " +
		"application/vnd.openxmlformats-package.core-properties+xml " +
		"application/vnd.openxmlformats-officedocument.extended-properties+xml " +
		"application/vnd.openxmlformats-officedocument.theme+xml " +
		"application/vnd.openxmlformats-officedocument.drawing+xml " +
		"application/vnd.openxmlformats-officedocument.vmldrawing " +
		"application/vnd.ms-excel.person+xml application/vnd.ms-excel.threadedcomments+xml " +
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml " +
		"application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml " +
		"application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml " +
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sharedstrings+xml " +
		"application/vnd.openxmlformats-officedocument.spreadsheetml.comments+xml " +
		"application/vnd.openxmlformats-officedocument.spreadsheetml.calcchain+xml " +
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheetmetadata+xml") {
		allowed[kind] = true
	}
	for _, row := range types.Rows {
		kind := strings.ToLower(row.Type)
		if row.XMLName.Space != types.XMLName.Space || !allowed[kind] {
			return errExactWorkbookUnsupported
		}
		switch row.XMLName.Local {
		case "Default":
			ext := strings.ToLower(row.Extension)
			if ext == "" || defaults[ext] != "" || row.Part != "" {
				return errExactWorkbookUnsupported
			}
			defaults[ext] = kind
		case "Override":
			name := strings.ToLower(strings.TrimPrefix(row.Part, "/"))
			if name == "" || path.Clean(name) != name || parts[name] == nil ||
				overrides[name] != "" || row.Extension != "" {
				return errExactWorkbookUnsupported
			}
			overrides[name] = kind
		default:
			return errExactWorkbookUnsupported
		}
	}
	if overrides["xl/workbook.xml"] != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml" {
		return errExactWorkbookUnsupported
	}
	for name := range parts {
		if name == "[content_types].xml" {
			continue
		}
		kind := overrides[name]
		if kind == "" {
			kind = defaults[strings.TrimPrefix(path.Ext(name), ".")]
		}
		if kind == "" || strings.Contains(kind, "macro") || strings.Contains(kind, "activex") {
			return errExactWorkbookUnsupported
		}
	}
	return nil
}
