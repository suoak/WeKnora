package mcpserver

import (
	"encoding/json"
	"errors"
	"strings"
)

var errPersonalTargets = errors.New("invalid personal MCP knowledge base selection")

// A selection narrows live authorization; it never grants access. Use the same
// selection at HTTP admission, tool admission and the existing read coordinator.
// Otherwise an unrelated KB's changing content can reject a bounded read.
func personalOperationTargets(name string, arguments map[string]any) ([]string, error) {
	switch name {
	case "list_documents":
		value, ok := arguments["knowledge_base_id"].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, errPersonalTargets
		}
		return []string{value}, nil
	case "search_knowledge", "grep_chunks":
		value, present := arguments["knowledge_base_ids"]
		if !present {
			return nil, nil
		}
		var raw []any
		switch ids := value.(type) {
		case []any:
			raw = ids
		case []string:
			for _, id := range ids {
				raw = append(raw, id)
			}
		default:
			return nil, errPersonalTargets
		}
		if len(raw) > 128 {
			return nil, errPersonalTargets
		}
		ids := make([]string, 0, len(raw))
		for _, item := range raw {
			id, ok := item.(string)
			if !ok || strings.TrimSpace(id) == "" {
				return nil, errPersonalTargets
			}
			ids = append(ids, id)
		}
		return ids, nil
	default:
		// Scope-wide operations retain their existing live scope checks.
		return nil, nil
	}
}

func personalHTTPRequestTargets(body []byte) ([]string, error) {
	var envelope struct {
		Method string `json:"method"`
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, errPersonalTargets
	}
	if envelope.Method != "tools/call" {
		return nil, nil
	}
	return personalOperationTargets(envelope.Params.Name, envelope.Params.Arguments)
}
