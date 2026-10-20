package core

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"net/http"
	"net/url"
	"path"
	"strings"
)

type applicationWorkbook struct{ files map[string]*zip.File }

func openApplicationWorkbook(data []byte) (*applicationWorkbook, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrApplicationTableIncomplete
	}
	w := &applicationWorkbook{files: map[string]*zip.File{}}
	var size uint64
	for _, f := range z.File {
		if f.UncompressedSize64 > 16<<20 {
			return nil, ErrApplicationTableIncomplete
		}
		if w.files[f.Name] != nil || path.Clean(f.Name) != f.Name || strings.HasPrefix(f.Name, "/") {
			return nil, ErrApplicationTableIncomplete
		}
		size += f.UncompressedSize64
		if size > 64<<20 {
			return nil, ErrApplicationTableIncomplete
		}
		w.files[f.Name] = f
		// Drawing anchors, charts and embedded objects need separate extraction.
		if strings.HasPrefix(f.Name, "xl/drawings/") || strings.HasPrefix(f.Name, "xl/charts/") || strings.HasPrefix(f.Name, "xl/embeddings/") {
			return nil, ErrApplicationTableMedia
		}
	}
	return w, nil
}
func (w *applicationWorkbook) read(name string) ([]byte, error) {
	f := w.files[name]
	if f == nil || f.UncompressedSize64 > 16<<20 {
		return nil, ErrApplicationTableIncomplete
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return readFeishuCapped(r, 16<<20)
}

type applicationWorkbookRelationship struct {
	ID     string `xml:"Id,attr"`
	Target string `xml:"Target,attr"`
	Type   string `xml:"Type,attr"`
	Mode   string `xml:"TargetMode,attr"`
}

func (w *applicationWorkbook) relationships(part string) (map[string]applicationWorkbookRelationship, error) {
	out := map[string]applicationWorkbookRelationship{}
	file := path.Join(path.Dir(part), "_rels", path.Base(part)+".rels")
	if w.files[file] == nil {
		return out, nil
	}
	b, err := w.read(file)
	if err != nil {
		return nil, err
	}
	var rels struct {
		Items []applicationWorkbookRelationship `xml:"Relationship"`
	}
	if xml.Unmarshal(b, &rels) != nil {
		return nil, ErrApplicationTableIncomplete
	}
	for _, r := range rels.Items {
		if r.ID == "" || out[r.ID].ID != "" {
			return nil, ErrApplicationTableIncomplete
		}
		out[r.ID] = r
	}
	return out, nil
}
func workbookTarget(part, target string) (string, error) {
	result := path.Clean(path.Join(path.Dir(part), target))
	if strings.HasPrefix(target, "/") {
		result = strings.TrimPrefix(path.Clean(target), "/")
	}
	if !strings.HasPrefix(result, "xl/") {
		return "", ErrApplicationTableIncomplete
	}
	return result, nil
}

// Sheet background pictures have no cell coordinate. Preserve their sheet
// association explicitly; do not invent a row. Hyperlinks retain their ref.
func (w *applicationWorkbook) enrich(ctx context.Context, s *ApplicationTableSnapshot) error {
	b, err := w.read("xl/workbook.xml")
	if err != nil {
		return err
	}
	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if xml.Unmarshal(b, &book) != nil || len(book.Sheets) != len(s.Tables) {
		return ErrApplicationTableIncomplete
	}
	rels, err := w.relationships("xl/workbook.xml")
	if err != nil {
		return err
	}
	used := map[string]bool{}
	seen := map[string]bool{}
	var assetBytes int64
	for _, sheet := range book.Sheets {
		var table *ApplicationTable
		for i := range s.Tables {
			if s.Tables[i].Title == sheet.Name {
				if table != nil {
					return ErrApplicationTableIncomplete
				}
				table = &s.Tables[i]
			}
		}
		if table == nil || seen[table.ID] {
			return ErrApplicationTableIncomplete
		}
		seen[table.ID] = true
		rel, ok := rels[sheet.ID]
		if !ok || rel.Mode == "External" {
			return ErrApplicationTableIncomplete
		}
		part, err := workbookTarget("xl/workbook.xml", rel.Target)
		if err != nil {
			return err
		}
		b, err = w.read(part)
		if err != nil {
			return err
		}
		links, err := w.relationships(part)
		if err != nil {
			return err
		}
		var source struct {
			Pictures []struct {
				ID string `xml:"id,attr"`
			} `xml:"picture"`
			Hyperlinks []struct {
				ID       string `xml:"id,attr"`
				Ref      string `xml:"ref,attr"`
				Location string `xml:"location,attr"`
			} `xml:"hyperlinks>hyperlink"`
		}
		if xml.Unmarshal(b, &source) != nil {
			return ErrApplicationTableIncomplete
		}
		for _, link := range source.Hyperlinks {
			target := link.Location
			if link.ID != "" {
				r, ok := links[link.ID]
				if !ok || !strings.HasSuffix(r.Type, "/hyperlink") {
					return ErrApplicationTableIncomplete
				}
				target = r.Target
				u, e := url.Parse(target)
				if e != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto") {
					return ErrApplicationTableIncomplete
				}
			}
			if link.Ref == "" || target == "" {
				return ErrApplicationTableIncomplete
			}
			table.Links = append(table.Links, ApplicationTableLink{Ref: link.Ref, Target: target})
		}
		for _, picture := range source.Pictures {
			r, ok := links[picture.ID]
			if !ok || r.Mode == "External" || !strings.HasSuffix(r.Type, "/image") {
				return ErrApplicationTableIncomplete
			}
			media, err := workbookTarget(part, r.Target)
			if err != nil || !strings.HasPrefix(media, "xl/media/") {
				return ErrApplicationTableIncomplete
			}
			data, err := w.read(media)
			if err != nil {
				return err
			}
			assetBytes += int64(len(data))
			if assetBytes > 32<<20 {
				return ErrApplicationTableIncomplete
			}
			used[media] = true
			h := sha256.Sum256(data)
			digest := hex.EncodeToString(h[:])
			asset := ApplicationAttachment{TableID: table.ID, FieldID: "sheet_background", RecordID: "sheet_background", Token: "xlsx_" + digest, Name: path.Base(media), MIME: http.DetectContentType(data), Size: int64(len(data)), SHA256: digest, Data: data}
			processor, _ := ctx.Value(applicationAttachmentProcessorKey{}).(ApplicationAttachmentProcessor)
			if processor == nil {
				return ErrApplicationTableMedia
			}
			parsed, err := processor(ctx, asset)
			if err != nil {
				return err
			}
			if parsed.SHA256 != asset.SHA256 || parsed.TableID != asset.TableID || parsed.RecordID != asset.RecordID || parsed.FieldID != asset.FieldID || parsed.Token != asset.Token || parsed.Text == "" || parsed.Parser == "" {
				return ErrApplicationTableIncomplete
			}
			parsed.Data = data
			s.Attachments = append(s.Attachments, parsed)
		}
	}
	for name := range w.files {
		if strings.HasPrefix(name, "xl/media/") && !used[name] {
			return ErrApplicationTableMedia
		}
	}
	return nil
}
