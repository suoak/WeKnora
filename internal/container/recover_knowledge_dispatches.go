package container

import (
	"context"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"time"
)

// Recover after handlers are registered and keep retrying when Redis was
// unavailable at startup. The cleaner stops and joins this loop on shutdown.
func recoverKnowledgeDispatches(knowledge interfaces.KnowledgeService, cleaner interfaces.ResourceCleaner) {
	recovery, ok := knowledge.(interfaces.KnowledgeDispatchRecovery)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			attempt, stop := context.WithTimeout(ctx, 45*time.Second)
			if err := recovery.RecoverKnowledgeDispatches(attempt); err != nil && ctx.Err() == nil {
				logger.Warnf(ctx, "Knowledge dispatch recovery incomplete; durable intent retained (database_or_queue_unavailable)")
			}
			stop()
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	cleaner.RegisterWithName("KnowledgeDispatchRecovery", func() error { cancel(); <-done; return nil })
}
