package types

import (
	"context"
	"sync"

	"gorm.io/gorm"
)

type legacyReadScopeKey struct{}
type legacyReadScope struct {
	mu       sync.Mutex
	kb       map[string]bool
	excluded bool
}

func RecordLegacyReadExclusion(ctx context.Context) {
	if scope, ok := ctx.Value(legacyReadScopeKey{}).(*legacyReadScope); ok {
		scope.mu.Lock()
		scope.excluded = true
		scope.mu.Unlock()
	}
}
func LegacyReadPartial(ctx context.Context) bool {
	if scope, ok := ctx.Value(legacyReadScopeKey{}).(*legacyReadScope); ok {
		scope.mu.Lock()
		defer scope.mu.Unlock()
		return scope.excluded
	}
	return false
}

// DiscardLegacyReadAttempt is used only after a read-only MCP attempt's entire
// response has been discarded, before restarting authorization from scratch.
// Keep the shared scope object so the final HTTP writer validates the new
// attempt's reads; retain the partial warning. It confers no read authority.
func DiscardLegacyReadAttempt(ctx context.Context) {
	if scope, ok := ctx.Value(legacyReadScopeKey{}).(*legacyReadScope); ok {
		scope.mu.Lock()
		scope.kb = map[string]bool{}
		scope.excluded = true
		scope.mu.Unlock()
	}
}

// Per-request provenance only, not another permission store or grant. IDs are
// recorded by actual server-side resource reads, never by identity headers.
func WithLegacyReadScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, legacyReadScopeKey{}, &legacyReadScope{kb: map[string]bool{}})
}
func recordLegacyRead(ctx context.Context, kb string) {
	if scope, ok := ctx.Value(legacyReadScopeKey{}).(*legacyReadScope); ok && kb != "" {
		scope.mu.Lock()
		scope.kb[kb] = true
		scope.mu.Unlock()
	}
}

// RecordLegacyCandidateRead records provenance of an already-authorized real
// candidate. It confers no permission and accepts no client identity fields.
func RecordLegacyCandidateRead(ctx context.Context, kb string) { recordLegacyRead(ctx, kb) }
func legacyReadKBs(ctx context.Context) []string {
	scope, ok := ctx.Value(legacyReadScopeKey{}).(*legacyReadScope)
	if !ok {
		return nil
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	ids := make([]string, 0, len(scope.kb))
	for id := range scope.kb {
		ids = append(ids, id)
	}
	return ids
}

// Recheck every source KB actually used by this request, including public KBs
// owned by another tenant. This never widens the original read authorization.
func CheckLegacyReadScope(db *gorm.DB) error {
	if !LegacyReadGuardsEnabled(db) {
		return nil
	}
	ids := legacyReadKBs(db.Statement.Context)
	for _, kb := range ids {
		if err := CheckPrivateKBRead(db, "", kb); err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		return nil
	}
	// Only this feature's source KBs participate. Ordinary non-Feishu write
	// responses must retain their existing semantics (e.g. successful delete).
	var invalid bool
	if err := db.Raw(`SELECT EXISTS(SELECT 1 FROM (
 SELECT knowledge_base_id FROM feishu_legacy_resources
 UNION SELECT knowledge_base_id FROM data_sources WHERE type IN ('feishu','feishu_drive','lark','lark_drive') OR legacy_access_state<>'unmanaged'
 ) s LEFT JOIN knowledge_bases k ON k.id=s.knowledge_base_id LEFT JOIN tenants t ON t.id=k.tenant_id
 WHERE s.knowledge_base_id IN ? AND (k.id IS NULL OR k.deleted_at IS NOT NULL OR t.id IS NULL OR t.status<>'active' OR t.deleted_at IS NOT NULL))`, ids).Scan(&invalid).Error; err != nil {
		return err
	}
	if invalid {
		return ErrLegacySourceRestricted
	}
	// Apply the workspace/source distinction again before browser output;
	// retain the batched strict check for API/MCP requests.
	if WorkspaceWebReadTenant(db.Statement.Context) != 0 {
		for _, kb := range ids {
			if err := CheckLegacySourceRead(db, "", kb); err != nil {
				return err
			}
		}
		return nil
	}
	var denied bool
	err := db.Raw(`SELECT EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE knowledge_base_id IN ? AND state IN ('hold','revoked')) OR EXISTS(SELECT 1 FROM data_sources WHERE knowledge_base_id IN ? AND legacy_access_state IN ('hold','revoked'))`, ids, ids).Scan(&denied).Error
	if err != nil {
		return err
	}
	if denied {
		return ErrLegacySourceRestricted
	}
	return nil
}
