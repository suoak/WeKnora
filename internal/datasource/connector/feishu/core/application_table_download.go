package core

import (
	"archive/zip"
	"bytes"
	"context"
)

// The API snapshot is the formal source artifact, not a lossy XLSX rendering.
// XLSX is additionally inspected for Sheet media invisible to the values API.
// Nothing is indexed until two complete reads agree and the caller rechecks
// source metadata. No secondary Knowledge or alternate queue is created.
func (c *Client) downloadApplicationTable(ctx context.Context, r PersonalResource) ([]byte, string, error) {
	snapshot, err := c.ReadApplicationTableCandidate(ctx, r)
	if err != nil {
		return nil, "", err
	}
	text, err := snapshot.IndexText()
	if err != nil {
		return nil, "", err
	}
	return []byte(text), SanitizeFileName(r.Title) + ".md", nil
}

func (c *Client) ReadApplicationTableCandidate(ctx context.Context, r PersonalResource) (*ApplicationTableSnapshot, error) {
	if err := requireApplicationIdentity(ctx); err != nil {
		return nil, err
	}
	if !applicationResourceAllowed(ctx, r.Type) || (r.Type != "sheet" && r.Type != "bitable") {
		return nil, ErrUnsupportedPersonalResource
	}
	var workbook *applicationWorkbook
	if r.Type == "sheet" {
		data, _, err := c.downloadResource(ctx, r)
		if err != nil {
			return nil, err
		}
		workbook, err = openApplicationWorkbook(data)
		if err != nil {
			return nil, err
		}
	}
	first, err := c.CollectApplicationTableSnapshot(ctx, r)
	if err != nil {
		return nil, err
	}
	second, err := c.CollectApplicationTableSnapshot(ctx, r)
	if err != nil {
		return nil, err
	}
	a, err := first.Fingerprint()
	if err != nil {
		return nil, err
	}
	b, err := second.Fingerprint()
	if err != nil {
		return nil, err
	}
	if a != b {
		return nil, ErrApplicationTableIncomplete
	}
	if workbook != nil {
		if err := workbook.enrich(ctx, second); err != nil {
			return nil, err
		}
	}
	return second, nil
}

func validateApplicationWorkbookMedia(data []byte) error {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ErrApplicationTableIncomplete
	}
	for _, f := range z.File {
		if len(f.Name) >= 9 && (hasPrefix(f.Name, "xl/media/") || hasPrefix(f.Name, "xl/drawings/") || hasPrefix(f.Name, "xl/embeddings/") || hasPrefix(f.Name, "xl/charts/")) {
			return ErrApplicationTableMedia
		}
	}
	return nil
}

func hasPrefix(s, prefix string) bool { return len(s) >= len(prefix) && s[:len(prefix)] == prefix }
