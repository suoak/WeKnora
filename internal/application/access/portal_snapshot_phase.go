package access

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
)

type portalSnapshotPhaseKey struct{}

type portalSnapshotEntry struct {
	done  chan struct{}
	value *types.EnterpriseReadSnapshot
	err   error
}

type portalSnapshotPhase struct {
	mu      sync.Mutex
	entries map[portalSnapshotIdentity]*portalSnapshotEntry
	bytes   int
}

type portalSnapshotIdentity struct {
	store                 any
	user, kb, privateUser string
	key                   uint64
	public, published     bool
	candidateVersion      bool
}

// WithPortalSnapshotReads shares immutable policy facts only inside one read
// operation. It grants no authority. Every model/output boundary must use
// WithoutPortalSnapshotReads for the existing live revalidation checks.
func WithPortalSnapshotReads(ctx context.Context) context.Context {
	if phase, _ := ctx.Value(portalSnapshotPhaseKey{}).(*portalSnapshotPhase); phase != nil {
		return ctx
	}
	return context.WithValue(ctx, portalSnapshotPhaseKey{}, &portalSnapshotPhase{
		entries: make(map[portalSnapshotIdentity]*portalSnapshotEntry),
	})
}

// WithoutPortalSnapshotReads forces fresh credential, publication, source and
// document-version reads. The inherited phase is never mutated or extended.
func WithoutPortalSnapshotReads(ctx context.Context) context.Context {
	return context.WithValue(ctx, portalSnapshotPhaseKey{}, (*portalSnapshotPhase)(nil))
}

// ReadPortalSnapshot merges identical in-flight policy reads and returns owned
// copies. Failures are not cached; identity and source ownership never cross a
// request, store, user, credential or snapshot mode.
func ReadPortalSnapshot(
	ctx context.Context, store any, user string, key uint64, kb string, public, published bool,
	load func() (*types.EnterpriseReadSnapshot, error),
) (*types.EnterpriseReadSnapshot, error) {
	phase, _ := ctx.Value(portalSnapshotPhaseKey{}).(*portalSnapshotPhase)
	if phase == nil {
		return load()
	}
	id := portalSnapshotIdentity{
		store, user, kb, types.PrivateKBReadUser(ctx), key, public, published, PortalCandidateVersionsRequested(ctx),
	}
	phase.mu.Lock()
	entry, found := phase.entries[id]
	if !found && len(phase.entries) >= 256 {
		phase.mu.Unlock()
		return load()
	}
	if !found {
		entry = &portalSnapshotEntry{done: make(chan struct{})}
		phase.entries[id] = entry
	}
	phase.mu.Unlock()
	if found {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-entry.done:
		}
	} else {
		value, err := load()
		size := 0
		if err == nil {
			var encoded []byte
			encoded, err = json.Marshal(value)
			size = len(encoded)
			entry.value = clonePortalSnapshot(value)
		}
		entry.err = err
		phase.mu.Lock()
		if err != nil || phase.bytes+size > 16<<20 {
			delete(phase.entries, id)
		} else {
			phase.bytes += size
		}
		close(entry.done)
		phase.mu.Unlock()
	}
	if entry.err != nil {
		return nil, entry.err
	}
	return clonePortalSnapshot(entry.value), nil
}

func clonePortalSnapshotPointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func clonePortalSnapshotPolicy(value types.EnterprisePolicy) types.EnterprisePolicy {
	value.ReviewedBy = clonePortalSnapshotPointer(value.ReviewedBy)
	value.ReviewedAt = clonePortalSnapshotPointer(value.ReviewedAt)
	return value
}

func clonePortalSnapshot(value *types.EnterpriseReadSnapshot) *types.EnterpriseReadSnapshot {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Credential.OwnerUserID = clonePortalSnapshotPointer(value.Credential.OwnerUserID)
	cloned.Credential.RevokedAt = clonePortalSnapshotPointer(value.Credential.RevokedAt)
	cloned.Credential.ExpiresAt = clonePortalSnapshotPointer(value.Credential.ExpiresAt)
	cloned.Credential.Capabilities = append(value.Credential.Capabilities[:0:0], value.Credential.Capabilities...)
	cloned.Space = clonePortalSnapshotPolicy(value.Space)
	cloned.KB = clonePortalSnapshotPolicy(value.KB)
	cloned.Documents = append(value.Documents[:0:0], value.Documents...)
	for i := range cloned.Documents {
		cloned.Documents[i].EnterprisePolicy = clonePortalSnapshotPolicy(value.Documents[i].EnterprisePolicy)
	}
	return &cloned
}
