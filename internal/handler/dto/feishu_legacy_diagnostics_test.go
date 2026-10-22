package dto

import (
	"encoding/json"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRestrictedSourceDiagnosticsDoNotLeakOrMutateCursor(t *testing.T) {
	ds := &types.DataSource{ID: "source", LastSyncCursor: types.JSON(`{"title":"restricted-canary"}`), LastSyncResult: types.JSON(`{"body":"restricted-canary"}`), ErrorMessage: "restricted-canary"}
	response := NewDataSourceResponse(ds)
	response.RestrictSyncDiagnostics()
	raw, err := json.Marshal(response)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "restricted-canary")
	require.Contains(t, string(ds.LastSyncCursor), "restricted-canary")
	require.Equal(t, "restricted-canary", ds.ErrorMessage)
}
