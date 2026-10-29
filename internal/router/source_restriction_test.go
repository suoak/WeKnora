package router

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type sourceRestrictionTaskRepo struct {
	interfaces.KnowledgeRepository
	reads atomic.Int32
}

func (r *sourceRestrictionTaskRepo) KnowledgeSourceRestriction(context.Context, uint64, string, string) (types.KnowledgeSourceRestriction, error) {
	r.reads.Add(1)
	return types.KnowledgeSourceRestriction{Restricted: true, SourceState: "hold"}, nil
}
func TestSourceRestrictionLiteStopsWithoutRetry(t *testing.T) {
	repo := &sourceRestrictionTaskRepo{}
	done := make(chan error, 1)
	handler := service.WrapKnowledgeSourceAdmission(repo, func(context.Context, *asynq.Task) error { t.Error("restricted task body executed"); return nil })
	executor := NewSyncTaskExecutor()
	executor.RegisterHandler(types.TypeDocumentProcess, handler)
	executor.SetFinalFailureHook(func(_ context.Context, _ *asynq.Task, err error) { done <- err })
	_, err := executor.Enqueue(asynq.NewTask(types.TypeDocumentProcess, []byte(`{"tenant_id":12,"knowledge_base_id":"kb","knowledge_id":"doc"}`)), asynq.MaxRetry(25))
	require.NoError(t, err)
	select {
	case stopped := <-done:
		require.True(t, errors.Is(stopped, asynq.SkipRetry))
	case <-time.After(3 * time.Second):
		t.Fatal("restricted task did not terminate immediately")
	}
	require.EqualValues(t, 1, repo.reads.Load())
}
