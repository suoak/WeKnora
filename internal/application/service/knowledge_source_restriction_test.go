package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type sourceAdmissionRepo struct {
	interfaces.KnowledgeRepository
	status types.KnowledgeSourceRestriction
	err    error
	scopes [][3]any
}

func (r *sourceAdmissionRepo) KnowledgeSourceRestriction(_ context.Context, tenant uint64, kb, kid string) (types.KnowledgeSourceRestriction, error) {
	r.scopes = append(r.scopes, [3]any{tenant, kb, kid})
	return r.status, r.err
}

func TestSourceAdmission(t *testing.T) {
	payload := []byte(`{"tenant_id":12,"knowledge_base_id":"kb","knowledge_id":"doc","secret":"never-archive"}`)
	for _, taskType := range []string{types.TypeDocumentProcess, types.TypeFeishuApplicationProcess, types.TypeManualProcess, types.TypeKnowledgePostProcess, types.TypeSummaryGeneration, types.TypeQuestionGeneration, types.TypeImageMultimodal, types.TypeKnowledgeAutoTag} {
		for _, state := range []string{"hold", "revoked"} {
			t.Run(taskType+state, func(t *testing.T) {
				repo := &sourceAdmissionRepo{status: types.KnowledgeSourceRestriction{Restricted: true, SourceState: state}}
				ran := false
				handler := WrapKnowledgeSourceAdmission(repo, func(context.Context, *asynq.Task) error { ran = true; return nil })
				err := handler(context.Background(), asynq.NewTask(taskType, payload))
				require.ErrorIs(t, err, asynq.SkipRetry)
				require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
				require.False(t, ran)
				require.Equal(t, [][3]any{{uint64(12), "kb", "doc"}}, repo.scopes)
			})
		}
	}
	t.Run("available and generic errors retain semantics", func(t *testing.T) {
		for _, want := range []error{nil, errors.New("ordinary transient error"), asynq.SkipRetry, types.ErrLegacySourceRestricted} {
			repo := &sourceAdmissionRepo{}
			calls := 0
			got := WrapKnowledgeSourceAdmission(repo, func(context.Context, *asynq.Task) error { calls++; return want })(context.Background(), asynq.NewTask(types.TypeDocumentProcess, payload))
			require.Equal(t, want, got)
			require.Equal(t, 1, calls)
		}
	})
	t.Run("database uncertainty never runs body or permanently stops task", func(t *testing.T) {
		repo := &sourceAdmissionRepo{err: errors.New("database unavailable")}
		got := WrapKnowledgeSourceAdmission(repo, func(context.Context, *asynq.Task) error { t.Fatal("body called"); return nil })(context.Background(), asynq.NewTask(types.TypeDocumentProcess, payload))
		require.ErrorIs(t, got, repo.err)
		require.NotErrorIs(t, got, asynq.SkipRetry)
	})
	t.Run("revocation during execution is rechecked", func(t *testing.T) {
		repo := &sourceAdmissionRepo{}
		got := WrapKnowledgeSourceAdmission(repo, func(context.Context, *asynq.Task) error {
			repo.status = types.KnowledgeSourceRestriction{Restricted: true, SourceState: "revoked"}
			return types.ErrLegacySourceRestricted
		})(context.Background(), asynq.NewTask(types.TypeDocumentProcess, payload))
		require.ErrorIs(t, got, asynq.SkipRetry)
		require.Len(t, repo.scopes, 2)
	})
	t.Run("unrelated and malformed tasks retain handlers", func(t *testing.T) {
		repo := &sourceAdmissionRepo{err: errors.New("must not query")}
		calls := 0
		handler := WrapKnowledgeSourceAdmission(repo, func(context.Context, *asynq.Task) error { calls++; return nil })
		require.NoError(t, handler(context.Background(), asynq.NewTask("other:task", payload)))
		require.NoError(t, handler(context.Background(), asynq.NewTask(types.TypeDocumentProcess, []byte(`{`))))
		require.Equal(t, 2, calls)
		require.Empty(t, repo.scopes)
	})
}

type sourceHeldDispatchRepo struct {
	interfaces.KnowledgeRepository
	interfaces.KnowledgeDispatchRepository
	op *types.TaskPendingOp
}

func (r *sourceHeldDispatchRepo) KnowledgeSourceRestriction(context.Context, uint64, string, string) (types.KnowledgeSourceRestriction, error) {
	return types.KnowledgeSourceRestriction{Restricted: true, SourceState: "hold"}, nil
}

func (r *sourceHeldDispatchRepo) ListKnowledgeDispatches(_ context.Context, after int64, _ int) ([]*types.TaskPendingOp, error) {
	if after == 0 {
		return []*types.TaskPendingOp{r.op}, nil
	}
	return nil, nil
}

func TestSourceRestrictionDurableIntentIsNotClaimedOrEnqueued(t *testing.T) {
	op := &types.TaskPendingOp{ID: 7, TenantID: 12, TaskType: types.TypeDocumentProcess, ScopeID: "doc", DedupKey: "document-doc", Op: "pending",
		Payload: []byte(`{"payload_version":1,"task":{"tenant_id":12,"knowledge_base_id":"kb","knowledge_id":"doc","dispatch_id":"document-doc","attempt":1}}`)}
	repo := &sourceHeldDispatchRepo{op: op}
	queue := &dispatchQueue{}
	s := &knowledgeService{repo: repo, task: queue}
	var stopped *types.SourceProcessingRestrictedError
	require.ErrorAs(t, s.publishKnowledgeDispatch(context.Background(), op), &stopped)
	require.NoError(t, s.RecoverKnowledgeDispatches(context.Background()))
	require.Empty(t, queue.snapshot())
	require.Equal(t, "pending", op.Op)
	require.Zero(t, op.FailCount)
	require.Nil(t, op.ClaimedAt)
}
