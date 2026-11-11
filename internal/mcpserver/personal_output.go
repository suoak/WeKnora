package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// A bare absolute path can occur in an authorized title/chunk/KB name as well
// as storage metadata. Keep relative Wiki slugs and knowhub: references intact.
var personalAbsolutePath = regexp.MustCompile(
	`(^|[\s"'=:\x60(\[{])` +
		`(?:[A-Za-z]:[\\/]|\\\\|/)` +
		`[^\s<>"'\x60\[\]{}(),;!?，。；！？、]+`,
)

// Error and panic values are opaque: neither is an authorized document result.
func personalToolBoundary(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (result *mcp.CallToolResult, err error) {
		defer func() {
			if recover() != nil {
				result, err = mcp.NewToolResultError("resource denied or unavailable"), nil
			}
		}()
		result, err = next(ctx, req)
		if err != nil || result == nil || result.IsError {
			return mcp.NewToolResultError("resource denied or unavailable"), nil
		}
		return result, nil
	}
}

func redactPersonalOutput(raw []byte) ([]byte, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber() // preserve uint64 identities exactly, including values > 2^53
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	var walk func(any) (any, error)
	walk = func(value any) (any, error) {
		switch v := value.(type) {
		case string:
			text, err := access.SanitizePortalText(v)
			if err != nil {
				return nil, err
			}
			return personalAbsolutePath.ReplaceAllString(text, "${1}[path omitted]"), nil
		case []any:
			for i := range v {
				next, err := walk(v[i])
				if err != nil {
					return nil, err
				}
				v[i] = next
			}
		case map[string]any:
			for key, old := range v {
				next, err := walk(old)
				if err != nil {
					return nil, err
				}
				v[key] = next
			}
		}
		return value, nil
	}
	value, err := walk(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
