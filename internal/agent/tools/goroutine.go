package tools

import (
	"context"

	"github.com/Tencent/WeKnora/internal/logger"
)

// GoRecovered runs fn on a new goroutine and contains a panic raised there.
//
// recover only covers the goroutine that calls it, so the barrier in
// executeRecovered (registry.go) protects the call to tool.Execute alone: a
// goroutine a tool starts for itself — an MCP client, a sandbox, a third-party
// SDK, a retrieval adapter — must install its own barrier, or one panic escapes
// every recover site and takes the whole server (and every conversation,
// upload and sync in flight) down with it.
//
// Deferred calls inside fn still run while a panic unwinds, so wait groups and
// cleanup are released. A caller that must also degrade the failed work into an
// error result uses RecoverGoroutine directly and records it in onPanic.
func GoRecovered(ctx context.Context, fn func()) {
	go func() {
		defer RecoverGoroutine(ctx, "", nil)
		fn()
	}()
}

// RecoverGoroutine is deferred by a goroutine that must not let a panic escape
// (either directly or through GoRecovered). It logs a controlled failure,
// then calls onPanic — when set — on that same goroutine so the caller can mark
// the failed unit of work: one MCP service, one knowledge base, one retrieval
// call. label is retained for call-site compatibility and is not logged.
//
// onPanic deliberately receives no panic value: a recovered value carries
// internals (paths, addresses) that must not reach the model or the user.
func RecoverGoroutine(ctx context.Context, label string, onPanic func()) {
	r := recover()
	if r == nil {
		return
	}
	// Provider panic values, stack frames and labels may contain credentials.
	logger.Error(ctx, "tool goroutine failed (panic)")
	if onPanic != nil {
		onPanic()
	}
}
