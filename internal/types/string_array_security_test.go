package types

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDocumentM5StringArrayDriverValues(t *testing.T) {
	for _, value := range []any{`["retrieve","enterprise_retrieve"]`, []byte(`["retrieve","enterprise_retrieve"]`)} {
		var scope StringArray
		require.NoError(t, scope.Scan(value))
		require.Equal(t, StringArray{"retrieve", "enterprise_retrieve"}, scope)
	}
	for _, value := range []any{true, 17, `{}`, `["retrieve",true]`, `garbage`} {
		scope := StringArray{"old-scope"}
		require.Error(t, scope.Scan(value))
		require.Nil(t, scope, "driver/decode errors cannot leave old authority")
	}
	for _, value := range []any{nil, "null", "[]", []byte("null")} {
		scope := StringArray{"old-scope"}
		require.NoError(t, scope.Scan(value))
		require.Empty(t, scope)
	}
}
