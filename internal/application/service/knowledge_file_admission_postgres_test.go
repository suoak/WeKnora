package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

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
		return nil, errors.New("FILE_ADMISSION_QUEUE_TOKEN_CANARY")
	}
	return &asynq.TaskInfo{ID: "file-admission-test", Queue: "default"}, nil
}

type fileAdmissionTenantFailure struct {
	interfaces.TenantRepository
	calls  int
	nonces []string
	svc    *knowledgeService
	t      *testing.T
}

func (r *fileAdmissionTenantFailure) GetTenantByID(ctx context.Context, tenant uint64) (*types.Tenant, error) {
	r.calls++
	execution, ok := types.ProcessingExecutionFromContext(ctx)
	require.True(r.t, ok, "actual ProcessDocument must enter the fenced worker")
	require.Equal(r.t, tenant, execution.TenantID)
	require.EqualValues(r.t, 1, execution.Generation)
	require.NotEmpty(r.t, execution.ExecutionID)
	r.nonces = append(r.nonces, execution.ExecutionID)
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
	require.Len(t, tenantFailure.nonces, 2)
	require.NotEqual(t, tenantFailure.nonces[0], tenantFailure.nonces[1], "actual retry must claim a new nonce")
	require.NoError(t, db.Where("knowledge_id = ?", knowledge.ID).Take(&fence).Error)
	require.EqualValues(t, 1, fence.Generation)
	// Release retains the execution identity for per-attempt cleanup, while
	// expiring its lease so a real retry can claim a different nonce.
	require.Equal(t, tenantFailure.nonces[1], fence.ExecutionID)
	var databaseNow time.Time
	require.NoError(t, db.Raw("SELECT CURRENT_TIMESTAMP").Row().Scan(&databaseNow))
	require.False(t, fence.LeaseUntil.After(databaseNow))
	require.False(t, fence.MainFinished)
	// An enqueue error cannot overwrite a delivery that already acquired a lease.
	claimed, err := fences.Claim(ctx, 1, knowledge.ID, 1, time.Minute)
	require.NoError(t, err)
	svc.markFileProcessingEnqueueFailed(ctx, knowledge, 1)
	stored, err = svc.repo.GetKnowledgeByID(ctx, 1, knowledge.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, stored.ParseStatus)
	require.NoError(t, fences.Release(ctx, claimed, false))
	// A broker error has no trusted rejection signal. Preserve pending so an
	// accepted delivery can still claim, and return a controlled non-success.
	queue.reject = true
	failed, err := svc.CreateKnowledgeFromFile(ctx, "kb-1", newMultipartFileHeader(t, "failed.txt", "different fresh upload"), nil, nil, "", nil, "", nil)
	require.ErrorIs(t, err, errFileProcessingEnqueueUnknown)
	require.Equal(t, types.ParseStatusPending, failed.ParseStatus)
	stored, err = svc.repo.GetKnowledgeByID(ctx, 1, failed.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusPending, stored.ParseStatus)
	require.Error(t, svc.ProcessDocument(ctx, queue.task))
	require.Equal(t, 3, tenantFailure.calls, "an ambiguously accepted delivery must still enter the actual worker")
	// A definite pre-enqueue failure uses the conditional update; the uncertain
	// queue error path above must never call it automatically.
	prequeue := &types.Knowledge{ID: "preenqueue-only", TenantID: 1, KnowledgeBaseID: "kb-1", Type: "file"}
	generation, err := svc.createFileProcessingKnowledge(ctx, prequeue)
	require.NoError(t, err)
	require.EqualValues(t, 1, generation)
	svc.markFileProcessingEnqueueFailed(ctx, prequeue, generation)
	stored, err = svc.repo.GetKnowledgeByID(ctx, 1, prequeue.ID)
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
