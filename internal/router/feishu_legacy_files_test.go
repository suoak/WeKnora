package router

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type legacyFileTestGate struct {
	denied bool
	calls  int
}

func (g *legacyFileTestGate) CheckLegacyFileRead(context.Context, uint64, string) (bool, error) {
	g.calls++
	if g.denied {
		return true, types.ErrLegacySourceRestricted
	}
	return true, nil
}
func TestLegacyFileRechecksBeforeEachRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest("GET", "/files/presigned", nil)
	gate := &legacyFileTestGate{}
	managed, allowed := legacyFilePreflight(c, gate, 1, "/synthetic/file")
	require.True(t, managed)
	require.True(t, allowed)
	reader := guardLegacyFileReader(c, gate, 1, "/synthetic/file", io.NopCloser(strings.NewReader("restrictedcanary")), managed)
	gate.denied = true
	buf := make([]byte, 4096)
	n, err := reader.Read(buf)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	require.Zero(t, n, "withdrawal must precede response body emission")
	_, allowed = legacyFilePreflight(c, gate, 1, "/synthetic/file")
	require.False(t, allowed)
	require.Equal(t, http.StatusForbidden, r.Code)
	require.Equal(t, "private, no-store", r.Header().Get("Cache-Control"))
}
