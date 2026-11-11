package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestPersonalOutputErrorsAndPanicsNeverReflectPayload(t *testing.T) {
	for _, kind := range []string{"error", "panic", "result"} {
		h := personalToolBoundary(func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if kind == "error" {
				return nil, errors.New("SYNTHETIC_PRIVATE_ERROR /home/private secret")
			}
			if kind == "panic" {
				panic("SYNTHETIC_PRIVATE_ERROR /home/private secret")
			}
			return mcp.NewToolResultError("SYNTHETIC_PRIVATE_ERROR /home/private secret"), nil
		})
		result, err := h(context.Background(), mcp.CallToolRequest{})
		require.NoError(t, err)
		require.True(t, result.IsError)
		b, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(b), "SYNTHETIC_PRIVATE_ERROR")
		require.NotContains(t, string(b), "/home/private")
	}
}

func TestPersonalOutputNoPhysicalPathsOrBearerURLs(t *testing.T) {
	value := map[string]any{
		"tenant_id": uint64(18446744073709551615),
		"content": "PUBLIC /home/SYNTHETIC_PATH/a.txt C:\\SYNTHETIC_PATH\\secret.txt " +
			"\\\\SYNTHETIC_PATH\\share /custom/SYNTHETIC_PATH/data /etc/passwd",
		"name":      "metadata https://files.invalid/file?sig=SYNTHETIC_SIGNATURE",
		"reference": "knowhub:tenant:1:kb:public-two:document:public-two-doc",
		"slug":      "concept/verified",
	}
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	clean, err := redactPersonalOutput(raw)
	require.NoError(t, err)
	for _, marker := range []string{"SYNTHETIC_PATH", "SYNTHETIC_SIGNATURE", "files.invalid", "/etc/passwd"} {
		require.NotContains(t, string(clean), marker)
	}
	require.Contains(t, string(clean), "18446744073709551615")
	require.Contains(t, string(clean), "knowhub:tenant:1:kb:public-two:document:public-two-doc")
	require.Contains(t, string(clean), "concept/verified")
	require.Contains(t, string(clean), "PUBLIC")
}

func TestPersonalOutputPreservesAuthorizedTextAroundRedactions(t *testing.T) {
	value := map[string]any{
		"content": "已授权正文：参见 concept/verified 和 src/pkg/main.go；机器路径 /home/app/private.txt，后续正文继续。",
		"slug":    "concept/verified",
		"ref":     "knowhub:tenant:1:kb:public-two:document:public-two-doc",
	}
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	clean, err := redactPersonalOutput(raw)
	require.NoError(t, err)
	var got map[string]string
	require.NoError(t, json.Unmarshal(clean, &got))
	require.Equal(t, "已授权正文：参见 concept/verified 和 src/pkg/main.go；机器路径 [path omitted]，后续正文继续。", got["content"])
	require.Equal(t, value["slug"], got["slug"])
	require.Equal(t, value["ref"], got["ref"])
}

func TestIsolatedOutputDoesNotChangeOriginalPersonalContent(t *testing.T) {
	value := map[string]string{"content": "original /home/app/private.txt，继续"}
	legacy, err := personalJSON(context.Background(), value)
	require.NoError(t, err)
	isolated, err := personalJSON(logger.WithIsolatedPersonalPayload(context.Background()), value)
	require.NoError(t, err)
	legacyText := legacy.Content[0].(mcp.TextContent).Text
	isolatedText := isolated.Content[0].(mcp.TextContent).Text
	require.Contains(t, legacyText, "/home/app/private.txt，继续")
	require.Contains(t, isolatedText, "[path omitted]，继续")
	require.NotContains(t, isolatedText, "/home/app/private.txt")
}
