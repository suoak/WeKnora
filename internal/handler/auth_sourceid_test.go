package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceIDPeerTrustDoesNotUseForwardedHeaders(t *testing.T) {
	r := httptest.NewRequest("POST", "http://mock/logout", nil)
	r.RemoteAddr = "192.0.2.10:1234"
	secret := strings.Repeat("mock", 8)
	r.Header.Set("X-KnowHub-SourceID-Secret", secret)
	require.True(t, trustedSourceIDPeer(r, "192.0.2.0/24", secret))
	r.Header.Set("X-Forwarded-For", "192.0.2.10")
	r.RemoteAddr = "198.51.100.5:1234"
	require.False(t, trustedSourceIDPeer(r, "192.0.2.0/24", secret))
	require.False(t, trustedSourceIDPeer(r, "", secret))
	require.False(t, trustedSourceIDPeer(r, "198.51.100.0/24,invalid", secret))
	r.Header.Set("X-KnowHub-SourceID-Secret", "wrong")
	require.False(t, trustedSourceIDPeer(r, "198.51.100.0/24", secret))
}
