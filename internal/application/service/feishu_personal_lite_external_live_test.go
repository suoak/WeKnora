//go:build anydoc && personal_exact_live

package service_test

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/router"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
)

func TestPersonalOriginalExactLitePublicConsumer(t *testing.T) {
	service.RunOriginalExactQueueAcceptance(t, func(handler asynq.HandlerFunc) interfaces.TaskEnqueuer {
		executor := router.NewSyncTaskExecutor()
		executor.RegisterHandler(types.TypeDocumentProcess, handler)
		return executor
	})
}
