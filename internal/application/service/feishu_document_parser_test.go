package service

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuDocumentParserPreferenceIsSourceAndItemScoped(t *testing.T) {
	config := &types.DataSourceConfig{Settings: map[string]interface{}{
		types.FeishuDocumentParserSettingsKey: map[string]types.FeishuDocumentParserOverride{
			"selected": {XLSXContextColumnCount: 1},
		},
	}}
	blob, err := config.ToJSON()
	require.NoError(t, err)
	ds := &types.DataSource{Type: "feishu", Config: blob}
	item := &types.FetchedItem{ExternalID: "selected", FileName: "source.xls"}
	overrides := map[string]string{"parser_semantic_chunk_max_chars": "7500", "unrelated": "preserved"}
	require.NoError(t, applyFeishuDocumentParserOverride(ds, item, "builtin", overrides))
	require.Equal(t, "1", overrides["xlsx_context_column_count"])
	require.Equal(t, "preserved", overrides["unrelated"])
	require.Error(t, applyFeishuDocumentParserOverride(ds, item, "builtin", nil))
	require.Error(t, applyFeishuDocumentParserOverride(nil, item, "builtin", overrides))
	require.Error(t, applyFeishuDocumentParserOverride(ds, nil, "builtin", overrides))
	require.Equal(t, "7500", overrides["parser_semantic_chunk_max_chars"])
	other := *item
	other.ExternalID = "sibling"
	untouched := map[string]string{}
	require.NoError(t, applyFeishuDocumentParserOverride(ds, &other, "builtin", untouched))
	require.Empty(t, untouched)
	require.Error(t, applyFeishuDocumentParserOverride(ds, item, "anydoc", overrides))
	wrong := *ds
	wrong.Type = "web"
	require.Error(t, applyFeishuDocumentParserOverride(&wrong, item, "builtin", overrides))
	item.IsDeleted = true
	require.Error(t, applyFeishuDocumentParserOverride(ds, item, "builtin", overrides))
	item.IsDeleted = false
	item.Metadata = map[string]string{"attachment": "true"}
	require.Error(t, applyFeishuDocumentParserOverride(ds, item, "builtin", overrides))
	item.Metadata, item.FileName = nil, "source.pdf"
	require.Error(t, applyFeishuDocumentParserOverride(ds, item, "builtin", overrides))
	item.FileName = "source.xls"
	bad := *config
	bad.Settings = map[string]interface{}{types.FeishuDocumentParserSettingsKey: "invalid"}
	raw, err := json.Marshal(bad)
	require.NoError(t, err)
	ds.Config = types.JSON(raw)
	require.Error(t, applyFeishuDocumentParserOverride(ds, item, "builtin", overrides))
}
