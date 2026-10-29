package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestFeishuTaskResultTerminatesOnlyExplicitSourceFences(t *testing.T) {
	for _, err := range []error{
		repository.ErrDerivedWriteRejected,
		fmt.Errorf("wrapped: %w", repository.ErrDerivedWriteRejected),
		&pgconn.PgError{Code: "40001", Message: "stale or unmanaged Feishu source write"},
	} {
		require.ErrorIs(t, FeishuTaskResult(err), asynq.SkipRetry)
	}
	for _, err := range []error{nil, errors.New("temporary model outage"), &pgconn.PgError{Code: "40001", Message: "could not serialize access due to concurrent update"}} {
		require.NotErrorIs(t, FeishuTaskResult(err), asynq.SkipRetry)
	}
}

type versionedSummaryRepository struct {
	*summaryColumnsRepo
	version string
}

func (r *versionedSummaryRepository) KnowledgeDerivedVersion(context.Context, string) (string, bool, error) {
	return r.version, true, nil
}
func (r *versionedSummaryRepository) BeginKnowledgeDerivedWrite(context.Context, string, uint64, string, func(context.Context, *types.DataSource) (context.Context, error)) (context.Context, bool, error) {
	panic("a stale task must stop before authorization or model work")
}

func TestFeishuDerivedSummaryDiscardsHistoricalAndSupersededPayloads(t *testing.T) {
	for _, version := range []string{"", "older-generation"} {
		t.Run("version="+version, func(t *testing.T) {
			repo := &versionedSummaryRepository{summaryColumnsRepo: &summaryColumnsRepo{t: t, knowledge: summaryKnowledge()}, version: "current-generation"}
			svc := &knowledgeService{repo: repo}
			payload, err := json.Marshal(types.SummaryGenerationPayload{TenantID: 1, KnowledgeBaseID: "kb-1", KnowledgeID: "k-1", SourceVersion: version})
			require.NoError(t, err)
			require.NoError(t, svc.ProcessSummaryGeneration(context.Background(), asynq.NewTask(types.TypeSummaryGeneration, payload)))
			require.Empty(t, repo.writes)
		})
	}
}

type failedSummaryStateRepository struct {
	*summaryColumnsRepo
	failure error
}

func (r *failedSummaryStateRepository) UpdateKnowledgeColumns(_ context.Context, _ string, values map[string]interface{}) error {
	r.writes = append(r.writes, values)
	return r.failure
}

func TestFeishuDerivedSummaryMissingModelPropagatesWriteFailure(t *testing.T) {
	writeErr := errors.New("summary state persistence unavailable")
	repo := &failedSummaryStateRepository{summaryColumnsRepo: &summaryColumnsRepo{t: t, knowledge: summaryKnowledge()}, failure: writeErr}
	kb := summaryKB()
	kb.SummaryModelID = ""
	svc := &knowledgeService{repo: repo, kbService: &reparseFailureKBService{kb: kb}}
	require.ErrorIs(t, svc.ProcessSummaryGeneration(context.Background(), summaryGenerationTask(t)), writeErr)
	require.Len(t, repo.writes, 1)
	require.Equal(t, types.SummaryStatusFailed, repo.writes[0]["summary_status"])
}
