package mcpserver

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPortalM6PersonalDefaultClosed(t *testing.T) {
	for _, srv := range []*PersonalServer{NewDisabledPersonalServer(), NewPersonalServer(true, nil)} {
		request := httptest.NewRequest("POST", "/api/v1/mcp/personal", nil)
		request.Header.Set("X-Enterprise-Enabled", "true")
		request.Header.Set("X-User-ID", "A")
		output := httptest.NewRecorder()
		srv.Handler().ServeHTTP(output, request)
		require.Equal(t, 403, output.Code)
	}
}
