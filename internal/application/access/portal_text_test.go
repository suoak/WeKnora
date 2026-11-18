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

func TestPortalTechnicalTextFidelity(t *testing.T) {
	for _, input := range []string{
		"| **NETCONF 操作** | `<edit-config>`, `<get-config>` | `<get>` |",
		"```xml\n<rpc><get/><filter type=\"subtree\"><interfaces/></filter></rpc>\n```",
		"show <interface-name>; std::vector<int>; a < b && b > c",
		"&lt;get-config&gt; &#60;get&#62; %3Cedit-config%3E; value=%2F; literal=%25",
		"中文 <自定义标签> and <ns:node key=\"value\">text</ns:node>",
		"<script>alert('literal code example')</script>",
	} {
		t.Run(input, func(t *testing.T) {
			text, err := SanitizePortalText(input)
			require.NoError(t, err)
			require.Equal(t, input, text)
			again, err := SanitizePortalText(text)
			require.NoError(t, err)
			require.Equal(t, text, again, "the output boundary must not decode or rewrite text again")
			require.NoError(t, ValidatePortalQuery(input))
		})
	}
}

func TestPortalTechnicalTextRedactsOnlyDestinationSpans(t *testing.T) {
	for _, destination := range []string{
		"https://files.invalid/object?token=SECRET",
		"HTTPS%3A%2F%2Ffiles.invalid%2Fobject%3Ftoken%3DSECRET",
		"https&#58;//files.invalid/object?token=SECRET",
		"https%253A%252F%252Ffiles.invalid%252Fobject%253Ftoken%253DSECRET",
		"h&#8203;ttps://files.invalid/object?token=SECRET",
		"h%25E2%2580%258Bttps://files.invalid/object?token=SECRET",
		"https:\\files.invalid/object?token=SECRET",
		"/files/private?token=SECRET",
	} {
		input := "`<get>` &lt;rpc&gt; %2F " + destination + " `<edit-config>`"
		text, err := SanitizePortalText(input)
		require.NoError(t, err, destination)
		require.Equal(t, "`<get>` &lt;rpc&gt; %2F [link omitted] `<edit-config>`", text)
		require.Error(t, ValidatePortalQuery(input), "a forbidden destination cannot be rewritten into a query")
		again, err := SanitizePortalText(text)
		require.NoError(t, err)
		require.Equal(t, text, again)
	}
	input := "[&lt;get&gt;](https://files.invalid/?token=SECRET) and `<get-config>`"
	text, err := SanitizePortalText(input)
	require.NoError(t, err)
	require.Equal(t, "&lt;get&gt; and `<get-config>`", text)
}
