package repository

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuLegacyProcessMetadataRetainsTypedOverrides(t *testing.T) {
	previous := &types.Knowledge{ID: "existing", Metadata: types.JSON(`{"owner":"A",
		"process_overrides":{"summary_enabled":false,"future":{"nested":[1,true,"text"]}}}`)}
	flattened := previous.GetMetadata()
	flattened["source_updated_at"] = "new-revision"
	blob, err := json.Marshal(flattened)
	require.NoError(t, err)
	candidate := &types.Knowledge{Metadata: types.JSON(blob)}
	require.NoError(t, legacyProcessMetadata(previous, candidate, true))
	overrides, err := candidate.ProcessOverrides()
	require.NoError(t, err)
	require.NotNil(t, overrides.SummaryEnabled)
	require.False(t, *overrides.SummaryEnabled)
	old, err := previous.Metadata.Map()
	require.NoError(t, err)
	next, err := candidate.Metadata.Map()
	require.NoError(t, err)
	require.Equal(t, old["process_overrides"], next["process_overrides"])
	require.Equal(t, "new-revision", next["source_updated_at"])
	require.NoError(t, legacyProcessMetadata(previous, candidate, false))
	before := append([]byte(nil), candidate.Metadata...)
	for _, normalize := range []bool{true, false} {
		changed := &types.Knowledge{
			ID:       "existing",
			Metadata: types.JSON(`{"process_overrides":{"summary_enabled":true}}`),
		}
		require.ErrorContains(
			t,
			legacyProcessMetadata(changed, candidate, normalize),
			"knowledge_process_config_changed",
		)
		require.Equal(t, before, []byte(candidate.Metadata), "rejected candidate must remain unchanged")
	}
}

func TestFeishuLegacyProcessMetadataRejectsCorruption(t *testing.T) {
	for _, metadata := range []string{
		`{"process_overrides":"map[summary_enabled:false]"}`,
		`{"process_overrides":{"summary_enabled":"invalid"}}`,
	} {
		previous := &types.Knowledge{ID: "existing", Metadata: types.JSON(metadata)}
		require.ErrorContains(
			t,
			legacyProcessMetadata(previous, &types.Knowledge{}, true),
			"knowledge_process_config_invalid",
		)
	}
	previous := &types.Knowledge{
		ID:       "existing",
		Metadata: types.JSON(`{"process_overrides":{"summary_enabled":false}}`),
	}
	require.ErrorContains(
		t,
		legacyProcessMetadata(previous, &types.Knowledge{}, true),
		"knowledge_process_config_changed",
	)
	previous.Metadata = types.JSON(`{}`)
	require.NoError(t, legacyProcessMetadata(previous, &types.Knowledge{}, true))
}
