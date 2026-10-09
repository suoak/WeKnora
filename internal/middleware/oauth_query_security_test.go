package middleware

import (
	"github.com/stretchr/testify/require"
	"net/url"
	"testing"
)

func TestOAuthProviderErrorQueryRedaction(t *testing.T) {
	raw := "error=synthetic-secret&error_description=synthetic-access&error_uri=synthetic-refresh&client_secret=synthetic-secret&ordinary=visible"
	redacted := sanitizeQuery(raw)
	for _, secret := range []string{"synthetic-secret", "synthetic-access", "synthetic-refresh"} {
		require.NotContains(t, redacted, secret)
	}
	fields, err := url.ParseQuery(redacted)
	require.NoError(t, err)
	require.Equal(t, "visible", fields.Get("ordinary"))
}
