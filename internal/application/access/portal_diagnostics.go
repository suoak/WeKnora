package access

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"sync"
)

type portalDiagnosticsKey struct{}

// Request-local only. No content, names or IDs of unavailable resources leave
// this collector. In particular, it is never a credential or a bypass flag.
type PortalDiagnostics struct {
	mu         sync.Mutex
	restricted map[string]bool
}

func WithPortalDiagnostics(ctx context.Context) (context.Context, *PortalDiagnostics) {
	if d, ok := ctx.Value(portalDiagnosticsKey{}).(*PortalDiagnostics); ok {
		return ctx, d
	}
	d := &PortalDiagnostics{restricted: map[string]bool{}}
	return context.WithValue(ctx, portalDiagnosticsKey{}, d), d
}
func recordPortalRestriction(ctx context.Context, kb string) {
	types.RecordLegacyReadExclusion(ctx)
	if d, ok := ctx.Value(portalDiagnosticsKey{}).(*PortalDiagnostics); ok {
		d.mu.Lock()
		d.restricted[kb] = true
		d.mu.Unlock()
	}
}
func (d *PortalDiagnostics) Partial() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.restricted) > 0
}

const PortalPartialWarning = "Partial knowledge scope: one or more sources require verification or have been revoked and were excluded. Results do not cover all knowledge."
