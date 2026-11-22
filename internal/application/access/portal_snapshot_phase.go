package access

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
)

type portalSnapshotPhaseKey struct{}

type portalSnapshotEntry struct {
	done chan struct{}
	data []byte
	err  error
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
	id := portalSnapshotIdentity{store, user, kb, types.PrivateKBReadUser(ctx), key, public, published}
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
		if err == nil {
			entry.data, err = json.Marshal(value)
		}
		entry.err = err
		phase.mu.Lock()
		if err != nil || phase.bytes+len(entry.data) > 16<<20 {
			delete(phase.entries, id)
		} else {
			phase.bytes += len(entry.data)
		}
		close(entry.done)
		phase.mu.Unlock()
	}
	if entry.err != nil {
		return nil, entry.err
	}
	var value *types.EnterpriseReadSnapshot
	if err := json.Unmarshal(entry.data, &value); err != nil {
		return nil, err
	}
	return value, nil
}
