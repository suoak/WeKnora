package service

import (
	"context"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
)

type wikiClaimLifecycleKey struct{}

type wikiClaimLifecycle struct {
	mu        sync.Mutex
	closed    bool
	finishers []func()
}

func (l *wikiClaimLifecycle) finish() {
	l.mu.Lock()
	l.closed = true
	finishers := l.finishers
	l.finishers = nil
	l.mu.Unlock()
	for _, finish := range finishers {
		finish()
	}
}

func (l *wikiClaimLifecycle) record(
	ctx context.Context, repo wikiOwnedClaimReleaser, payload WikiIngestPayload, claims map[int64]time.Time,
) {
	if len(claims) == 0 {
		return
	}
	captured := make(map[int64]time.Time, len(claims))
	for id, stamp := range claims {
		captured[id] = stamp
	}
	var once sync.Once
	done := make(chan struct{})
	release := func() {
		once.Do(func() {
			defer close(done)
			count, err := releaseAbortedWikiClaims(ctx, repo, payload, captured)
			if err != nil {
				logger.Warnf(ctx, "Wiki claim lifecycle cleanup failed kb=%s: %v", payload.KnowledgeBaseID, err)
			} else if count > 0 {
				logger.Infof(ctx, "Wiki claim lifecycle released %d owned operations kb=%s", count, payload.KnowledgeBaseID)
			}
		})
	}
	// Cancellation may make Asynq retire its worker before a model call has
	// returned. Release metadata immediately, independently of that call.
	stop := context.AfterFunc(ctx, release)
	finish := func() { stop(); release(); <-done }
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		finish()
		return
	}
	l.finishers = append(l.finishers, finish)
	l.mu.Unlock()
}
