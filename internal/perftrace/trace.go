// Package perftrace records bounded, anonymous MCP stage durations, never inputs.
package perftrace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type key struct{}

// Stage is a fixed label; callers cannot put knowledge or credential data in it.
type Stage uint8

const (
	Auth Stage = iota
	Resolve
	Snapshot
	DimensionSQL
	BM25SQL
	VectorSQL
	Embedding
	Merge
	Serialize
	stageCount
)

var names = [stageCount]string{
	"auth", "resolve", "snapshot", "dimension_sql", "bm25_sql", "vector_sql", "embedding", "merge", "serialize",
}

// Trace belongs to one HTTP request; no user ID, query, result, or Key is stored.
type Trace struct {
	mu       sync.Mutex
	id       string
	start    time.Time
	authDone bool
	duration [stageCount]time.Duration
	count    [stageCount]uint64
}

// New creates a server-generated identifier, independent of caller headers.
func New(ctx context.Context) (context.Context, *Trace) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return ctx, nil
	}
	t := &Trace{id: hex.EncodeToString(id[:]), start: time.Now()}
	return context.WithValue(ctx, key{}, t), t
}

// Measure returns a no-op outside traced personal MCP requests.
func Measure(ctx context.Context, stage Stage) func() {
	t, _ := ctx.Value(key{}).(*Trace)
	if t == nil || stage >= stageCount {
		return func() {}
	}
	start := time.Now()
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.duration[stage] += time.Since(start)
		t.count[stage]++
	}
}

// Authenticated marks entry into the authenticated handler, before scope resolve.
func Authenticated(ctx context.Context) {
	t, _ := ctx.Value(key{}).(*Trace)
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.authDone {
		t.duration[Auth] = time.Since(t.start)
		t.count[Auth] = 1
		t.authDone = true
	}
}

// Summary contains overlapping inclusive stages; durations must not be summed.
func (t *Trace) Summary() map[string]any {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	stages := make(map[string]any, stageCount)
	for stage, name := range names {
		stages[name] = map[string]any{"ms": float64(t.duration[stage].Microseconds()) / 1000, "calls": t.count[stage]}
	}
	return map[string]any{
		"request": t.id, "total_ms": float64(time.Since(t.start).Microseconds()) / 1000,
		"authenticated": t.authDone, "inclusive_stages": stages,
	}
}
