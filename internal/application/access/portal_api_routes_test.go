package access

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPortalDocumentedTechnicalAPIRoutes(t *testing.T) {
	for _, input := range []string{
		"POST `/v2/widgets/activate/`",
		"| GET | `/v3/widgets/read_status/` | 查询状态 |",
		"## Example\n\n- 接口名称：`createWidget`\n- URL：`/v2/widgets/create/`\n- 方法：`POST`\n\nOther text",
		"URL: /v12/widgets/configuration/\nMethod: PATCH",
	} {
		got, err := SanitizePortalTechnicalOutputText(input)
		require.NoError(t, err)
		require.Equal(t, input, got)
		again, err := SanitizePortalTechnicalOutputText(got)
		require.NoError(t, err)
		require.Equal(t, got, again)
		ordinary, err := SanitizePortalOutputText(input)
		require.NoError(t, err)
		require.NotEqual(t, input, ordinary, "ordinary output policy remains the opt-out rollback")
	}
}

func TestPortalTechnicalAPIRoutesKeepContainment(t *testing.T) {
	for _, path := range []string{
		"/home/app/private.txt", "/etc/passwd", "C:\\private\\file.txt", "//storage.invalid/opaque",
		"/v2/files/private", "/v2/widgets/download", "/v2/credentials/read", "/v2/etc/passwd",
		"/v2/widgets/private.txt", "/v2/widgets/../private", "/v2/widgets/%2e%2e/private",
		"/v2/widgets/opaque?signature=SYNTHETIC_SECRET", "/v2/widgets/opaque#SYNTHETIC_SECRET",
		"/v2/widgets/abcd-efgh-ijkl-mnop", "/v2/widgets/item12345678",
		"/v2/widgets/aabbccddeeff00112233445566778899/",
		"%2Fv2%2Fwidgets%2Factivate%2F", "/v2/wid\u200bgets/activate/",
		"/api/v1/knowledge/download?token=SYNTHETIC_SECRET", "/r/SYNTHETIC_SECRET",
		"https://storage.invalid/v2/widgets/activate/?token=SYNTHETIC_SECRET",
	} {
		t.Run(path, func(t *testing.T) {
			got, err := SanitizePortalTechnicalOutputText("POST `" + path + "` remaining text")
			require.NoError(t, err)
			require.NotContains(t, got, path)
			require.NotContains(t, got, "SYNTHETIC_SECRET")
			require.NotContains(t, got, "storage.invalid")
			require.Contains(t, got, "remaining text")
		})
	}
	for _, input := range []string{
		"/v2/widgets/activate/", "ordinary /v2/widgets/activate/ text", "URL: /v2/widgets/activate/",
	} {
		got, err := SanitizePortalTechnicalOutputText(input)
		require.NoError(t, err)
		require.Contains(t, got, "[path omitted]", "a bare or unclassified path is not an API grant")
	}
}
