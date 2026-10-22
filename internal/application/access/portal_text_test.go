package access

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPortalV1TextProjection(t *testing.T) {
	for _, input := range []string{
		"ordinary office text",
		"policy [reference](https://example.test/page) remains readable",
		"policy https://example.test/page?signature=SECRET and /files/private?token=SECRET remains readable",
		"policy ![private](https://storage.test/file?token=SECRET) remains readable",
		"policy [file][secret]\n[secret]: s3://bucket/private?token=SECRET\nremains readable",
		"policy <img src=\"https://storage.test/file?token=SECRET\"> remains readable",
		"policy HTTPS%3A%2F%2Fexample.test%2Fprivate%3Ftoken%3DSECRET remains readable",
		"policy https&#58;//example.test/private?token=SECRET remains readable",
		"policy /r/SECRET and resource://SECRET remains readable",
		"policy h\u200bttps://example.test/SECRET remains readable",
		"policy https:\\example.test/SECRET remains readable",
	} {
		t.Run(input, func(t *testing.T) {
			text, err := SanitizePortalText(input)
			require.NoError(t, err)
			require.True(t, SafeDocumentText(text))
			require.NotContains(t, text, "SECRET")
			require.NotContains(t, text, "example.test")
			if strings.HasPrefix(input, "policy") {
				require.Contains(t, text, "policy")
				require.Contains(t, text, "readable")
			}
		})
	}
	text, err := SanitizePortalText("read [public manual](https://example.test)")
	require.NoError(t, err)
	require.Equal(t, "read public manual", text)
	for _, bad := range []string{"unsafe ![unfinished", string([]byte{0xff}), "nul\x00", "%252525253A", strings.Repeat("x", 2*1024*1024+1)} {
		_, err := SanitizePortalText(bad)
		require.Error(t, err)
	}
	// Preserve the strict legacy guard; callers must explicitly opt in.
	require.False(t, SafeDocumentText("https://example.test"))
}
