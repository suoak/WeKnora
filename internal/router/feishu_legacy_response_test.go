package router

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

type legacyResponseTestGate struct{ denied bool }

func (g *legacyResponseTestGate) CheckLegacyReadScope(context.Context) error {
	if g.denied {
		return types.ErrLegacySourceRestricted
	}
	return nil
}
func TestLegacyFileResponseBoundaryStopsWithdrawnContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, streamed := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		gate := &legacyResponseTestGate{}
		writer := &legacyScopedResponse{ResponseWriter: ctx.Writer, ctx: context.Background(), gate: gate}
		if streamed {
			_, err := writer.WriteString("previously authorized prefix")
			require.NoError(t, err)
		}
		gate.denied = true
		if !streamed {
			writer.Header().Set("Content-Length", "999999")
		}
		n, err := writer.WriteString("withdrawn-sensitive-canary")
		require.Zero(t, n)
		require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
		require.NotContains(t, recorder.Body.String(), "withdrawn-sensitive-canary")
		if !streamed {
			require.Equal(t, 403, recorder.Code)
			require.Empty(t, recorder.Header().Get("Content-Length"), "deny must not retain the old file length and stall clients")
		}
	}
}
