package types

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFeishuDocumentParserPreferencesRoundTripAndRejectUnknownControls(t *testing.T) {
	for _, value := range []string{
		`null`, `[]`, `{"": {"xlsx_context_column_count":1}}`,
		`{"doc":{"xlsx_context_column_count":0}}`, `{"doc":{"xlsx_context_column_count":65}}`,
		`{"doc":{"xlsx_context_column_count":1.5}}`, `{"doc":{"xlsx_context_column_count":"1"}}`,
		`{"doc":{"xlsx_context_column_count":1,"parser_semantic_chunk_max_chars":99999}}`,
		`{"doc":{"xlsx_context_column_count":1,"url":"https://example.invalid"}}`,
		`{"doc":{"xlsx_context_column_count":1,"engine":"anydoc"}}`,
	} {
		config := &DataSourceConfig{Settings: map[string]interface{}{
			FeishuDocumentParserSettingsKey: json.RawMessage(value),
		}}
		_, err := config.ToJSON()
		require.Error(t, err)
		require.NotContains(t, err.Error(), "https://")
	}
	config := &DataSourceConfig{Settings: map[string]interface{}{
		FeishuDocumentParserSettingsKey: map[string]FeishuDocumentParserOverride{
			"document": {XLSXContextColumnCount: 1},
		},
	}}
	blob, err := config.ToJSON()
	require.NoError(t, err)
	parsed, err := (&DataSource{Config: blob}).ParseConfig()
	require.NoError(t, err)
	preferences, err := parsed.FeishuDocumentParserOverrides()
	require.NoError(t, err)
	require.Equal(t, 1, preferences["document"].XLSXContextColumnCount)
	var nilConfig *DataSourceConfig
	_, err = nilConfig.FeishuDocumentParserOverrides()
	require.NoError(t, err)
	config.Settings[FeishuDocumentParserSettingsKey] = map[string]FeishuDocumentParserOverride{
		strings.Repeat("x", 513): {XLSXContextColumnCount: 1},
	}
	_, err = config.ToJSON()
	require.Error(t, err)
}
