package service

import (
	"bytes"
	"encoding/xml"
	"io"
)

// Only referenced relationships assign an image to a worksheet. A second,
// unused relationship to an already used asset must not establish ownership.
func standaloneImageReferences(parts map[string][]byte, owner, imageNode, drawingNode string) error {
	refs := map[string]string{}
	d := xml.NewDecoder(bytes.NewReader(parts[owner]))
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errExactWorkbookUnsupported
		}
		node, ok := token.(xml.StartElement)
		if !ok || (node.Name.Local != imageNode && node.Name.Local != drawingNode) {
			continue
		}
		kind, attribute := "image", "id"
		if node.Name.Local == drawingNode {
			kind = "drawing"
		}
		if imageNode == "blip" {
			attribute = "embed"
		}
		id := ""
		for _, attr := range node.Attr {
			if attr.Name.Space == exactWorkbookRelNamespace && attr.Name.Local == attribute {
				if id != "" {
					return errExactWorkbookUnsupported
				}
				id = attr.Value
			}
		}
		if id == "" || (refs[id] != "" && refs[id] != kind) {
			return errExactWorkbookUnsupported
		}
		refs[id] = kind
	}
	rels, err := standaloneWorkbookRelations(parts, owner)
	if err != nil {
		return err
	}
	for _, rel := range rels {
		if rel.Type != exactWorkbookRelNamespace+"/image" && rel.Type != exactWorkbookRelNamespace+"/drawing" {
			continue
		}
		if refs[rel.ID] == "" || rel.Type != exactWorkbookRelNamespace+"/"+refs[rel.ID] {
			return errExactWorkbookUnsupported
		}
		delete(refs, rel.ID)
	}
	if len(refs) != 0 {
		return errExactWorkbookUnsupported
	}
	return nil
}
