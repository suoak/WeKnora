package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
)

// Attachment bytes never appear in JSON, logs, or the indexed Markdown. The
// caller persists them atomically with the private source manifest.
type ApplicationAttachment struct {
	TableID  string `json:"table_id"`
	FieldID  string `json:"field_id"`
	RecordID string `json:"record_id"`
	Token    string `json:"resource_token"`
	Name     string `json:"name"`
	MIME     string `json:"mime"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Text     string `json:"parsed_text"`
	Parser   string `json:"parser"`
	Data     []byte `json:"-"`
}
type applicationAttachmentProcessorKey struct{}
type ApplicationAttachmentProcessor func(context.Context, ApplicationAttachment) (ApplicationAttachment, error)

func WithApplicationAttachmentProcessor(ctx context.Context, process ApplicationAttachmentProcessor) context.Context {
	return context.WithValue(ctx, applicationAttachmentProcessorKey{}, process)
}

func (c *Client) collectApplicationAttachments(ctx context.Context, tableID, fieldID, recordID string, raw json.RawMessage) ([]ApplicationAttachment, error) {
	process, _ := ctx.Value(applicationAttachmentProcessorKey{}).(ApplicationAttachmentProcessor)
	if process == nil {
		return nil, ErrApplicationTableMedia
	}
	if string(raw) == "null" || len(raw) == 0 {
		return nil, nil
	}
	var files []struct {
		Token string `json:"file_token"`
		Name  string `json:"name"`
		MIME  string `json:"type"`
		Size  int64  `json:"size"`
	}
	if json.Unmarshal(raw, &files) != nil {
		return nil, ErrApplicationTableIncomplete
	}
	out := []ApplicationAttachment{}
	seen := map[string]bool{}
	total := int64(0)
	for _, file := range files {
		if !resourceTokenPattern.MatchString(file.Token) || file.Name == "" || file.Size < 1 || file.Size > 16<<20 || seen[file.Token] {
			return nil, ErrApplicationTableIncomplete
		}
		seen[file.Token] = true
		total += file.Size
		if total > 32<<20 {
			return nil, ErrApplicationTableIncomplete
		}
		a := ApplicationAttachment{TableID: tableID, FieldID: fieldID, RecordID: recordID, Token: file.Token, Name: file.Name, MIME: file.MIME, Size: file.Size}
		extra := map[string]any{"bitablePerm": map[string]any{"tableId": tableID, "attachments": map[string]any{fieldID: map[string]any{recordID: []string{file.Token}}}}}
		encoded, _ := json.Marshal(extra)
		data, err := c.downloadRawBytesOperation(ctx, "download_bitable_attachment", "/open-apis/drive/v1/medias/"+url.PathEscape(file.Token)+"/download?extra="+url.QueryEscape(string(encoded)))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) != file.Size {
			return nil, ErrApplicationTableIncomplete
		}
		hash := sha256.Sum256(data)
		a.SHA256 = hex.EncodeToString(hash[:])
		a.Data = data
		parsed, err := process(ctx, a)
		if err != nil {
			return nil, err
		}
		// A callback supplies parsing evidence, never changes source ownership.
		if parsed.SHA256 != a.SHA256 || parsed.TableID != a.TableID || parsed.FieldID != a.FieldID || parsed.RecordID != a.RecordID || parsed.Token != a.Token || parsed.Size != a.Size || parsed.Name != a.Name || parsed.MIME != a.MIME || parsed.Text == "" || parsed.Parser == "" {
			return nil, ErrApplicationTableIncomplete
		}
		parsed.Data = data
		out = append(out, parsed)
	}
	return out, nil
}
