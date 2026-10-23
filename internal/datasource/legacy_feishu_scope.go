package datasource

import (
	"errors"
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

var ErrLegacyFeishuSourceNotAllowed = errors.New("legacy Feishu source excluded by deployment synchronization allowlist")

// LegacyFeishuSourceAllowed limits rollout admission, including old deliveries.
// Empty preserves compatibility; "none" blocks all legacy sources. Every entry
// must be a canonical UUID: a malformed list fails closed, never partially opens.
// This does not drain in-flight work or override suspension, ACLs or user pause.
func LegacyFeishuSourceAllowed(connectorType, sourceID string) bool {
	switch connectorType {
	case types.ConnectorTypeFeishu, types.ConnectorTypeFeishuDrive,
		types.ConnectorTypeLark, types.ConnectorTypeLarkDrive:
	default:
		return true
	}
	raw := os.Getenv("FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS")
	if raw == "" {
		return true
	}
	allowed := false
	for _, entry := range strings.Split(raw, ",") {
		id := strings.TrimSpace(entry)
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.String() != id {
			return false
		}
		allowed = allowed || id == sourceID
	}
	return allowed
}
