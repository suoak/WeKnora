package types

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// FeishuDocumentParserSettingsKey names source-owned per-document Excel preferences.
const FeishuDocumentParserSettingsKey = "feishu_document_parser_overrides_v1"

// FeishuDocumentParserOverride carries a bounded builtin Excel parsing preference.
type FeishuDocumentParserOverride struct {
	XLSXContextColumnCount int `json:"xlsx_context_column_count"`
}

// FeishuDocumentParserOverrides validates bounded, non-secret parser preferences.
// Resource IDs are scoped by the owning datasource; these values grant no access.
func (d *DataSourceConfig) FeishuDocumentParserOverrides() (map[string]FeishuDocumentParserOverride, error) {
	if d == nil || d.Settings == nil {
		return nil, nil
	}
	raw, exists := d.Settings[FeishuDocumentParserSettingsKey]
	if !exists {
		return nil, nil
	}
	invalid := errors.New("invalid Feishu document parser overrides")
	data, err := json.Marshal(raw)
	if err != nil || len(data) > 64*1024 {
		return nil, invalid
	}
	var overrides map[string]FeishuDocumentParserOverride
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&overrides) != nil || overrides == nil || len(overrides) > 128 {
		return nil, invalid
	}
	for id, override := range overrides {
		if id == "" || len(id) > 512 || strings.TrimSpace(id) != id || strings.ContainsAny(id, "\x00\r\n") ||
			override.XLSXContextColumnCount < 1 || override.XLSXContextColumnCount > 64 {
			return nil, invalid
		}
	}
	return overrides, nil
}
