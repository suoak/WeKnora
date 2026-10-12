package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

type fileAdmissionQueue struct {
	task   *asynq.Task
	reject bool
	calls  int
}

func (q *fileAdmissionQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.calls++
	q.task = task
	if q.reject {
		return nil, errors.New("controlled_queue_unavailable")
	}
	return &asynq.TaskInfo{ID: "file-admission-test", Queue: "default"}, nil
}

type fileAdmissionTenantFailure struct {
	interfaces.TenantRepository
	calls int
	svc   *knowledgeService
	t     *testing.T
}

func (r *fileAdmissionTenantFailure) GetTenantByID(ctx context.Context, tenant uint64) (*types.Tenant, error) {
	r.calls++
	execution, ok := types.ProcessingExecutionFromContext(ctx)
	require.True(r.t, ok, "actual ProcessDocument must enter the fenced worker")
	require.Equal(r.t, tenant, execution.TenantID)
	require.EqualValues(r.t, 1, execution.Generation)
	require.NotEmpty(r.t, execution.ExecutionID)
	return nil, errors.New("controlled_tenant_read_retry")
}

func TestA3ProcessingExecutionPostgresFileUploadAdmission(t *testing.T) {
	db, svc, fences := processingExecutionPG(t)
	// Isolated synthetic schema only, not a schema3023 upgrade assertion.
	require.NoError(t, db.Exec("DROP TABLE knowledges").Error)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.KnowledgeTag{}, &types.KnowledgeTagRelation{}))
	require.NoError(t, db.Exec("CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, tenant_id BIGINT NOT NULL, deleted_at TIMESTAMPTZ)").Error)
	require.NoError(t, db.Exec("INSERT INTO knowledge_bases (id, tenant_id) VALUES ('kb-1',1),('kb-other',2)").Error)
	svc.config = &config.Config{KnowledgeBase: &config.KnowledgeBaseConfig{FencedFileAdmission: true}}
	svc.kbService = &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1", TenantID: 1}}
	files := &createKnowledgeFileServiceStub{}
	queue := &fileAdmissionQueue{}
	svc.fileSvc = files
	svc.task = queue
	ctx := newCreateKnowledgeFileContext()
	knowledge, err := svc.CreateKnowledgeFromFile(ctx, "kb-1", newMultipartFileHeader(t, "fresh.txt", "fresh upload"), map[string]string{"source": "synthetic"}, nil, "", nil, "", nil)
	require.NoError(t, err)
	require.NotNil(t, knowledge)
	require.Equal(t, 1, queue.calls)
	var payload types.DocumentProcessPayload
	require.NoError(t, json.Unmarshal(queue.task.Payload(), &payload))
	require.EqualValues(t, 1, payload.Generation)
	require.Equal(t, knowledge.ID, payload.KnowledgeID)
	require.Equal(t, knowledge.FilePath, payload.FilePath)
	stored, err := svc.repo.GetKnowledgeByID(ctx, 1, knowledge.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, stored.ParseStatus)
	require.Equal(t, knowledge.Metadata, stored.Metadata)
	var fence repository.ProcessingFence
	require.NoError(t, db.Where("knowledge_id = ?", knowledge.ID).Take(&fence).Error)
	require.EqualValues(t, 1, fence.Generation)
	require.Empty(t, fence.ExecutionID)
	tenantFailure := &fileAdmissionTenantFailure{svc: svc, t: t}
	svc.tenantRepo = tenantFailure
	// Real queued payload, public worker entry, SQL claim/release and retry. The
	// unavailable tenant dependency is intentionally a failure, not mocked success.
	require.Error(t, svc.ProcessDocument(ctx, queue.task))
	require.Error(t, svc.ProcessDocument(ctx, queue.task))
	require.Equal(t, 2, tenantFailure.calls)
	require.NoError(t, db.Where("knowledge_id = ?", knowledge.ID).Take(&fence).Error)
	require.EqualValues(t, 1, fence.Generation)
	require.Empty(t, fence.ExecutionID)
	require.False(t, fence.MainFinished)
	// An enqueue error cannot overwrite a delivery that already acquired a lease.
	claimed, err := fences.Claim(ctx, 1, knowledge.ID, 1, config.DefaultDocumentProcessTimeout)
	require.NoError(t, err)
	svc.markFileProcessingEnqueueFailed(ctx, knowledge, 1)
	stored, err = svc.repo.GetKnowledgeByID(ctx, 1, knowledge.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, stored.ParseStatus)
	require.NoError(t, fences.Release(ctx, claimed, false))
	// Definite unclaimed admission failure remains failed without an entire-row Save.
	queue.reject = true
	failed, err := svc.CreateKnowledgeFromFile(ctx, "kb-1", newMultipartFileHeader(t, "failed.txt", "different fresh upload"), nil, nil, "", nil, "", nil)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusFailed, failed.ParseStatus)
	stored, err = svc.repo.GetKnowledgeByID(ctx, 1, failed.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusFailed, stored.ParseStatus)
	// SQL failure must roll back the Knowledge as well as the generation and
	// remove the already stored file; there is no orphaned pending admission.
	require.NoError(t, db.Exec(`CREATE FUNCTION reject_file_admission() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'controlled_admission_rejected'; END $$`).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_file_admission BEFORE INSERT ON knowledge_processing_executions FOR EACH ROW EXECUTE FUNCTION reject_file_admission()`).Error)
	var before, after int64
	require.NoError(t, db.Model(&types.Knowledge{}).Count(&before).Error)
	queue.reject = false
	rejected, err := svc.CreateKnowledgeFromFile(ctx, "kb-1", newMultipartFileHeader(t, "rejected.txt", "third upload"), nil, nil, "", nil, "", nil)
	require.Error(t, err)
	require.Nil(t, rejected)
	require.Equal(t, 1, files.deleteCalls)
	require.NoError(t, db.Model(&types.Knowledge{}).Count(&after).Error)
	require.Equal(t, before, after)
	require.Equal(t, 2, queue.calls, "failed SQL transaction must not enqueue")
	// A caller cannot create a row in another tenant, even before SQL starts.
	provider := svc.repo.(interface {
		CreateKnowledgeForProcessing(context.Context, *types.Knowledge) (int64, error)
	})
	_, err = provider.CreateKnowledgeForProcessing(ctx, &types.Knowledge{ID: "cross-tenant", TenantID: 2, KnowledgeBaseID: "kb-1"})
	require.ErrorIs(t, err, repository.ErrProcessingFence)
	_, err = provider.CreateKnowledgeForProcessing(ctx, &types.Knowledge{ID: "cross-kb", TenantID: 1, KnowledgeBaseID: "kb-other"})
	require.ErrorIs(t, err, repository.ErrProcessingFence)
}
