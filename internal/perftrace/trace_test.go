package perftrace

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestTraceAnonymousBoundedAndConcurrent(t *testing.T) {
	ctx, trace := New(context.Background())
	if trace == nil {
		t.Fatal("random identifier unavailable")
	}
	Authenticated(ctx)
	Authenticated(ctx)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); Measure(ctx, Snapshot)() }()
	}
	wg.Wait()
	Measure(context.Background(), Snapshot)()
	Measure(ctx, stageCount)()
	encoded, err := json.Marshal(trace.Summary())
	if err != nil {
		t.Fatal(err)
	}
	if trace.count[Snapshot] != 10 || trace.count[Auth] != 1 || len(encoded) > 2048 {
		t.Fatal("unbounded or inconsistent stage metrics")
	}
	for _, marker := range []string{"Authorization", "user_id", "query", "content", "api_key"} {
		if strings.Contains(string(encoded), marker) {
			t.Fatalf("unexpected sensitive field %s", marker)
		}
	}
	_, other := New(ctx)
	if other.id == trace.id || len(trace.id) != 32 {
		t.Fatal("identifier reused")
	}
}
