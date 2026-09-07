package middleware

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPortalIdentityRoutesAllowTenantlessJWTContext(t *testing.T) {
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/portal/stages"},
		{http.MethodGet, "/api/v1/portal/spaces"},
		{http.MethodPost, "/api/v1/portal/spaces/7/access-requests"},
		{http.MethodPost, "/api/v1/portal/spaces/7/interaction/resolve"},
		{http.MethodGet, "/api/v1/me/tenant-access-requests"},
		{http.MethodPost, "/api/v1/me/tenant-access-requests/request-1/cancel"},
		{http.MethodGet, "/api/v1/system/admin/portal/spaces"},
	} {
		require.Truef(t, isTenantOptionalAPI(tc.path, tc.method), "%s %s", tc.method, tc.path)
	}
	require.False(t, isTenantOptionalAPI("/api/v1/knowledge-bases", http.MethodGet),
		"business content routes must continue to require a tenant")
}
