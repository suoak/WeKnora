package datasource

import (
	"errors"
	"os"
	"strconv"

	"github.com/Tencent/WeKnora/internal/types"
)

// ErrLegacyFeishuSyncSuspended reports legacy feishu sync suspended.
var ErrLegacyFeishuSyncSuspended = errors.New(
	"legacy Feishu Wiki/Drive synchronization is temporarily suspended; " +
		"existing knowledge remains available",
)

// LegacyFeishuSyncSuspended is a deployment admission barrier, not a live drain
// protocol. All old producers must exit before starting a suspended deployment.
// Empty/unset preserves existing behavior; malformed nonempty settings fail closed.
// Personal application_grant and other connectors do not use this barrier.
func LegacyFeishuSyncSuspended(connectorType string) bool {
	switch connectorType {
	case types.ConnectorTypeFeishu,
		types.ConnectorTypeFeishuDrive,
		types.ConnectorTypeLark,
		types.ConnectorTypeLarkDrive:
	default:
		return false
	}
	raw := os.Getenv("FEISHU_LEGACY_SYNC_SUSPENDED")
	if raw == "" {
		return false
	}
	enabled, err := strconv.ParseBool(raw)
	return err != nil || enabled
}
