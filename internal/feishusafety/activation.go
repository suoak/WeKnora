package feishusafety

import (
	"context"
	"os"
	"strconv"
)

// Enabled defaults to hard protection for legacy background synchronization.
// Malformed nonempty activation is treated as enabled, so missing configuration
// closes Feishu rather than silently bypassing protection.
func Enabled() bool {
	raw := os.Getenv("FEISHU_SYNC_SAFETY_ENABLED")
	if raw == "" {
		return true
	}
	value, err := strconv.ParseBool(raw)
	return err != nil || value
}

// LegacyContext marks only legacy source synchronization, never online queries
// or personal application grants. Marking does not itself authorize any request.
func LegacyContext(ctx context.Context, kind, id string) context.Context {
	if Enabled() {
		switch kind {
		case "feishu", "feishu_drive", "lark", "lark_drive":
			return WithScope(ctx, id)
		}
	}
	return ctx
}

// CheckLegacy prevents producers from filling a frozen synchronization queue.
func CheckLegacy(ctx context.Context, kind, id string) error {
	return Check(LegacyContext(ctx, kind, id))
}
