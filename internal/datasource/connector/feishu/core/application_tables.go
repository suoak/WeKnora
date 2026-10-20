package core

// This full snapshot reader is deliberately separate from blocks.go's bounded
// embedded-table preview. Any limit/error discards the snapshot, never its tail.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

var (
	// ErrApplicationTableIncomplete reports application table incomplete.
	ErrApplicationTableIncomplete = errors.New("feishu_table_content_incomplete")
	// ErrApplicationTableMedia rejects table media that cannot be completely represented.
	ErrApplicationTableMedia = errors.New("feishu_table_media_not_supported")
)

const (
	applicationTableBytes = 64 << 20
	applicationTableCells = 2000000
)

// ApplicationTableSnapshot defines application table snapshot state.
type ApplicationTableSnapshot struct {
	Version     int                     `json:"version"`
	Resource    PersonalResource        `json:"resource"`
	Tables      []ApplicationTable      `json:"tables"`
	Attachments []ApplicationAttachment `json:"attachments,omitempty"`
}

// ApplicationTable defines application table state.
type ApplicationTable struct {
	Links    []ApplicationTableLink   `json:"links,omitempty"`
	ID       string                   `json:"id"`
	Title    string                   `json:"title"`
	Fields   []ApplicationTableField  `json:"fields,omitempty"`
	Records  []ApplicationTableRecord `json:"records,omitempty"`
	Metadata json.RawMessage          `json:"metadata,omitempty"`
	Cells    []ApplicationTableRow    `json:"rows,omitempty"`
}

// ApplicationTableLink defines application table link state.
type ApplicationTableLink struct {
	Ref    string `json:"ref"`
	Target string `json:"target"`
}

// ApplicationTableField defines application table field state.
type ApplicationTableField struct {
	ID       string          `json:"field_id"`
	Name     string          `json:"field_name"`
	Type     int             `json:"type"`
	Property json.RawMessage `json:"property,omitempty"`
}

// ApplicationTableRecord defines application table record state.
type ApplicationTableRecord struct {
	ID     string                     `json:"record_id"`
	Fields map[string]json.RawMessage `json:"fields"`
}

// ApplicationTableRow defines application table row state.
type ApplicationTableRow struct {
	Number   int               `json:"row"`
	Values   []json.RawMessage `json:"values"`
	Formulas []json.RawMessage `json:"formulas"`
}

// CollectApplicationTableSnapshot reads only with an explicit application
// principal. Caller must still revalidate revision/ACL before persisting it.
func (c *Client) CollectApplicationTableSnapshot(
	ctx context.Context,
	r PersonalResource,
) (*ApplicationTableSnapshot, error) {
	if err := requireApplicationIdentity(ctx); err != nil {
		return nil, err
	}
	if !resourceTokenPattern.MatchString(r.Token) {
		return nil, ErrApplicationTableIncomplete
	}
	s := &ApplicationTableSnapshot{Version: 1, Resource: r}
	var err error
	switch r.Type {
	case "sheet":
		err = c.collectApplicationSheets(ctx, s)
	case "bitable":
		err = c.collectApplicationBitable(ctx, s)
	default:
		return nil, ErrUnsupportedPersonalResource
	}
	if err != nil {
		return nil, err
	}
	if len(s.Tables) == 0 {
		return nil, ErrApplicationTableIncomplete
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if len(b) > applicationTableBytes {
		return nil, ErrApplicationTableIncomplete
	}
	return s, nil
}

// Pagination is a completeness boundary, not a preview hint. A repeated token,
// duplicate identity, inconsistent total or an empty continuation is fatal.
func (c *Client) applicationList(ctx context.Context, path string) ([]json.RawMessage, error) {
	var items []json.RawMessage
	seen := map[string]bool{}
	page := ""
	bytes := 0
	total := -1
	for n := 0; n < 100000; n++ {
		p := path + "?page_size=100"
		if page != "" {
			p += "&page_token=" + url.QueryEscape(page)
		}
		var resp struct {
			Data struct {
				Items     []json.RawMessage `json:"items"`
				HasMore   bool              `json:"has_more"`
				PageToken string            `json:"page_token"`
				Total     *int              `json:"total"`
			} `json:"data"`
		}
		if err := c.doRequest(ctx, "application_table_list", http.MethodGet, p, nil, &resp); err != nil {
			return nil, err
		}
		d := resp.Data
		if d.Items == nil {
			return nil, ErrApplicationTableIncomplete
		}
		if d.Total != nil {
			if total >= 0 && total != *d.Total {
				return nil, ErrApplicationTableIncomplete
			}
			total = *d.Total
		}
		for _, v := range d.Items {
			bytes += len(v)
			if bytes > applicationTableBytes {
				return nil, ErrApplicationTableIncomplete
			}
			items = append(items, v)
		}
		if !d.HasMore {
			if total >= 0 && len(items) != total {
				return nil, ErrApplicationTableIncomplete
			}
			return items, nil
		}
		if len(d.Items) == 0 || d.PageToken == "" || seen[d.PageToken] {
			return nil, ErrApplicationTableIncomplete
		}
		page = d.PageToken
		seen[page] = true
	}
	return nil, ErrApplicationTableIncomplete
}

func (c *Client) collectApplicationBitable(ctx context.Context, s *ApplicationTableSnapshot) error {
	base := "/open-apis/bitable/v1/apps/" + s.Resource.Token + "/tables"
	tables, err := c.applicationList(ctx, base)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	budget := 0
	for _, raw := range tables {
		var t struct {
			ID   string `json:"table_id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &t) != nil || !resourceTokenPattern.MatchString(t.ID) || seen[t.ID] {
			return ErrApplicationTableIncomplete
		}
		seen[t.ID] = true
		table := ApplicationTable{ID: t.ID, Title: t.Name, Metadata: raw}
		fields, err := c.applicationList(ctx, base+"/"+t.ID+"/fields")
		if err != nil {
			return err
		}
		names := map[string]bool{}
		ids := map[string]bool{}
		for _, v := range fields {
			budget += len(v)
			if budget > applicationTableBytes {
				return ErrApplicationTableIncomplete
			}
			var f ApplicationTableField
			if json.Unmarshal(v, &f) != nil || f.ID == "" || f.Name == "" || names[f.Name] || ids[f.ID] {
				return ErrApplicationTableIncomplete
			}
			// Attachments must be downloaded, parsed/OCR'd and linked to their record
			// before this gate can open. A URL or token alone is never complete content.
			if f.Type == 17 && ctx.Value(applicationAttachmentProcessorKey{}) == nil {
				return ErrApplicationTableMedia
			}
			switch f.Type {
			case 1, 2, 3, 4, 5, 7, 11, 13, 15, 17, 18, 19, 20, 21, 22, 23, 1001, 1002, 1003, 1004, 1005:
			default:
				return ErrApplicationTableIncomplete
			}
			names[f.Name] = true
			ids[f.ID] = true
			table.Fields = append(table.Fields, f)
		}
		if len(fields) == 0 {
			return ErrApplicationTableIncomplete
		}
		records, err := c.applicationList(ctx, base+"/"+t.ID+"/records")
		if err != nil {
			return err
		}
		recordIDs := map[string]bool{}
		for _, v := range records {
			budget += len(v)
			if budget > applicationTableBytes {
				return ErrApplicationTableIncomplete
			}
			var rec ApplicationTableRecord
			if json.Unmarshal(v, &rec) != nil || rec.ID == "" || recordIDs[rec.ID] || rec.Fields == nil {
				return ErrApplicationTableIncomplete
			}
			for _, field := range table.Fields {
				if field.Type == 17 {
					assets, err := c.collectApplicationAttachments(
						ctx,
						table.ID,
						field.ID,
						rec.ID,
						rec.Fields[field.Name],
					)
					if err != nil {
						return err
					}
					s.Attachments = append(s.Attachments, assets...)
					var assetBytes int64
					for _, asset := range s.Attachments {
						assetBytes += asset.Size
					}
					if assetBytes > 32<<20 {
						return ErrApplicationTableIncomplete
					}
					rec.Fields[field.Name], err = json.Marshal(assets)
					if err != nil {
						return err
					}
				}
			}
			for name, raw := range rec.Fields {
				if !names[name] {
					return ErrApplicationTableIncomplete
				}
				var value any
				if json.Unmarshal(raw, &value) != nil {
					return ErrApplicationTableIncomplete
				}
				if containsTableMedia(value) {
					return ErrApplicationTableMedia
				}
			}
			recordIDs[rec.ID] = true
			table.Records = append(table.Records, rec)
		}
		s.Tables = append(s.Tables, table)
	}
	return nil
}

func spreadsheetColumn(n int) string {
	r := ""
	for n > 0 {
		n--
		r = string(rune('A'+n%26)) + r
		n /= 26
	}
	return r
}

func (c *Client) collectApplicationSheets(ctx context.Context, s *ApplicationTableSnapshot) error {
	path := "/open-apis/sheets/v3/spreadsheets/" + s.Resource.Token + "/sheets/query"
	var resp struct {
		Data struct {
			Sheets []json.RawMessage `json:"sheets"`
		} `json:"data"`
	}
	if err := c.doRequest(ctx, "application_sheet_metadata", http.MethodGet, path, nil, &resp); err != nil {
		return err
	}
	seen := map[string]bool{}
	cells := 0
	budget := 0
	var revision *int64
	for _, raw := range resp.Data.Sheets {
		var sh struct {
			ID           string `json:"sheet_id"`
			Title        string `json:"title"`
			ResourceType string `json:"resource_type"`
			Grid         struct {
				Rows    int `json:"row_count"`
				Columns int `json:"column_count"`
			} `json:"grid_properties"`
		}
		if json.Unmarshal(raw, &sh) != nil || !resourceTokenPattern.MatchString(sh.ID) || seen[sh.ID] {
			return ErrApplicationTableIncomplete
		}
		seen[sh.ID] = true
		if sh.ResourceType != "" && sh.ResourceType != "sheet" {
			return ErrApplicationTableIncomplete
		}
		rows, cols := sh.Grid.Rows, sh.Grid.Columns
		if rows < 1 || cols < 1 || cols > 16384 || rows > applicationTableCells/cols {
			return ErrApplicationTableIncomplete
		}
		cells += rows * cols
		if cells > applicationTableCells {
			return ErrApplicationTableIncomplete
		}
		table := ApplicationTable{ID: sh.ID, Title: sh.Title, Metadata: raw}
		for start := 1; start <= rows; start += 200 {
			end := min(rows, start+199)
			rg := sh.ID + "!A" + strconv.Itoa(start) + ":" + spreadsheetColumn(cols) + strconv.Itoa(end)
			var matrices [2][][]json.RawMessage
			for i, mode := range []string{"ToString", "Formula"} {
				var values struct {
					Data struct {
						Revision   *int64 `json:"revision"`
						ValueRange struct {
							Range  string              `json:"range"`
							Values [][]json.RawMessage `json:"values"`
						} `json:"valueRange"`
					} `json:"data"`
				}
				p := "/open-apis/sheets/v2/spreadsheets/" + s.Resource.Token + "/values/" + url.PathEscape(
					rg,
				) + "?valueRenderOption=" + mode + "&dateTimeRenderOption=FormattedString"
				if err := c.doRequest(ctx, "application_sheet_values", http.MethodGet, p, nil, &values); err != nil {
					return err
				}
				if values.Data.ValueRange.Range != rg {
					return ErrApplicationTableIncomplete
				}
				if values.Data.Revision != nil {
					if revision != nil && *revision != *values.Data.Revision {
						return ErrApplicationTableIncomplete
					}
					value := *values.Data.Revision
					revision = &value
				}
				matrices[i] = values.Data.ValueRange.Values
				if len(matrices[i]) > end-start+1 {
					return ErrApplicationTableIncomplete
				}
				for _, row := range matrices[i] {
					if len(row) > cols {
						return ErrApplicationTableIncomplete
					}
					for _, v := range row {
						budget += len(v)
						if budget > applicationTableBytes {
							return ErrApplicationTableIncomplete
						}
						// Structured media cannot be flattened into a string or ignored.
						var x any
						if json.Unmarshal(v, &x) != nil {
							return ErrApplicationTableIncomplete
						}
						if containsTableMedia(x) {
							return ErrApplicationTableMedia
						}
					}
				}
			}
			for offset := 0; offset < max(len(matrices[0]), len(matrices[1])); offset++ {
				row := ApplicationTableRow{Number: start + offset}
				if offset < len(matrices[0]) {
					row.Values = matrices[0][offset]
				}
				if offset < len(matrices[1]) {
					row.Formulas = matrices[1][offset]
				}
				table.Cells = append(table.Cells, row)
			}
		}
		s.Tables = append(s.Tables, table)
	}
	return nil
}

func containsTableMedia(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if k == "fileToken" || k == "file_token" || k == "image" || k == "image_token" {
				return true
			}
			if k == "type" {
				if t, ok := v.(string); ok && (strings.Contains(strings.ToLower(t), "image") || t == "attachment") {
					return true
				}
			}
			if containsTableMedia(v) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if containsTableMedia(v) {
				return true
			}
		}
	}
	return false
}

// IndexText repeats document/table/field context for every source row. Raw JSON
// keeps rich values, stable IDs, types, formulas and relation identifiers intact.
// It contains no credentials or expiring media URLs (media is rejected above).
func (s *ApplicationTableSnapshot) IndexText() (string, error) {
	var b strings.Builder
	b.WriteString("<!-- knowhub-feishu-table-snapshot-v1 -->\n")
	fmt.Fprintf(
		&b,
		"# %s\nSource: %s\nResource type: %s\n",
		s.Resource.Title,
		s.Resource.URL,
		s.Resource.Type,
	)
	for _, t := range s.Tables {
		fmt.Fprintf(&b, "\n## %s [%s]\nMetadata: %s\n", t.Title, t.ID, t.Metadata)
		for _, link := range t.Links {
			fmt.Fprintf(
				&b,
				"\n### Document: %s | Sheet: %s [%s] | Hyperlink: %s\nTarget: %s\n",
				tableHeading(s.Resource.Title),
				tableHeading(t.Title),
				t.ID,
				link.Ref,
				link.Target,
			)
		}
		for _, asset := range s.Attachments {
			if asset.TableID == t.ID && asset.FieldID == "sheet_background" {
				encoded, _ := json.Marshal(asset)
				fmt.Fprintf(
					&b,
					"\n### Document: %s | Sheet: %s [%s] | Sheet background "+
						"image\nAttachment: %s\n",
					tableHeading(s.Resource.Title),
					tableHeading(t.Title),
					t.ID,
					encoded,
				)
			}
		}
		headerRows := []ApplicationTableRow{}
		for _, row := range t.Cells {
			if applicationRowNonempty(row) {
				headerRows = append(headerRows, row)
				if len(headerRows) == 3 {
					break
				}
			}
		}
		header, _ := json.Marshal(headerRows)
		schema, _ := json.Marshal(t.Fields)
		if len(header) > 16384 {
			return "", ErrApplicationTableIncomplete
		}
		fmt.Fprintf(&b, "Field schema: %s\n", schema)
		for _, rec := range t.Records {
			fmt.Fprintf(
				&b,
				"\n### Document: %s | Table: %s [%s] | Record: %s\n",
				tableHeading(s.Resource.Title),
				tableHeading(t.Title),
				t.ID,
				rec.ID,
			)
			for _, field := range t.Fields {
				value := rec.Fields[field.Name]
				if len(value) == 0 {
					value = json.RawMessage("null")
				}
				fmt.Fprintf(
					&b,
					"\n#### Field: %s [%s] | Type: %d\nValue: %s\n",
					tableHeading(field.Name),
					field.ID,
					field.Type,
					value,
				)
				if b.Len() > applicationTableBytes {
					return "", ErrApplicationTableIncomplete
				}
			}
			if b.Len() > applicationTableBytes {
				return "", ErrApplicationTableIncomplete
			}
		}
		for _, row := range t.Cells {
			if !applicationRowNonempty(row) {
				continue
			}
			v, _ := json.Marshal(row.Values)
			f, _ := json.Marshal(row.Formulas)
			fmt.Fprintf(
				&b,
				"\n### Document: %s | Sheet: %s [%s] | Row: %d\nFirst rows / header "+
					"context: %s\nValues (A onward): %s\nFormulas (A onward): %s\n",
				tableHeading(s.Resource.Title),
				tableHeading(t.Title),
				t.ID,
				row.Number,
				header,
				v,
				f,
			)
			for col := 0; col < max(len(row.Values), len(row.Formulas)); col++ {
				var value, formula json.RawMessage
				if col < len(row.Values) {
					value = row.Values[col]
				}
				if col < len(row.Formulas) {
					formula = row.Formulas[col]
				}
				if !applicationRowNonempty(
					ApplicationTableRow{
						Values:   []json.RawMessage{value},
						Formulas: []json.RawMessage{formula},
					},
				) {
					continue
				}
				labels := []json.RawMessage{}
				for _, h := range headerRows {
					if col < len(h.Values) {
						labels = append(labels, h.Values[col])
					}
				}
				label, _ := json.Marshal(labels)
				fmt.Fprintf(
					&b,
					"\n#### Column: %s | Header context: %s\nValue: %s\nFormula: %s\n",
					spreadsheetColumn(col+1),
					label,
					value,
					formula,
				)
				if b.Len() > applicationTableBytes {
					return "", ErrApplicationTableIncomplete
				}
			}
			if b.Len() > applicationTableBytes {
				return "", ErrApplicationTableIncomplete
			}
		}
	}
	if b.Len() > applicationTableBytes {
		return "", ErrApplicationTableIncomplete
	}
	return b.String(), nil
}

func tableHeading(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }

func applicationRowNonempty(row ApplicationTableRow) bool {
	for _, values := range [][]json.RawMessage{row.Values, row.Formulas} {
		for _, v := range values {
			s := strings.TrimSpace(string(v))
			if s != "" && s != "null" && s != "\"\"" && s != "[]" {
				return true
			}
		}
	}
	return false
}

// Fingerprint hashes the canonical snapshot for completeness comparison.
func (s *ApplicationTableSnapshot) Fingerprint() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
