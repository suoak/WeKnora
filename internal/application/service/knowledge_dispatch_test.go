package service

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type dispatchFaultRepo struct {
	interfaces.KnowledgeRepository
	interfaces.KnowledgeDispatchRepository
	commitUnknown, rollback, ackFailure bool
	readFailure                         error
}

func (r *dispatchFaultRepo) GetKnowledgeByID(
	ctx context.Context,
	tenant uint64,
	id string,
) (*types.Knowledge, error) {
	if r.readFailure != nil {
		return nil, r.readFailure
	}
	return r.KnowledgeRepository.GetKnowledgeByID(ctx, tenant, id)
}

func (r *dispatchFaultRepo) GetKnowledgeTags(
	context.Context,
	[]string,
) (map[string][]*types.KnowledgeTag, error) {
	return nil, nil
}

func (r *dispatchFaultRepo) AcceptKnowledgeDispatch(
	ctx context.Context,
	k *types.Knowledge,
	op *types.TaskPendingOp,
) error {
	if r.rollback {
		return &types.KnowledgeDispatchAdmissionError{Cause: errors.New("DB rollback before commit")}
	}
	err := r.KnowledgeDispatchRepository.AcceptKnowledgeDispatch(ctx, k, op)
	if err == nil && r.commitUnknown {
		return errors.New("commit timeout unknown")
	}
	return err
}

func (r *dispatchFaultRepo) SetKnowledgeDispatchState(ctx context.Context, id int64, state string) error {
	if r.ackFailure && state == "dispatched" {
		return errors.New("ack write failed")
	}
	return r.KnowledgeDispatchRepository.SetKnowledgeDispatchState(ctx, id, state)
}

type dispatchQueue struct {
	mu    sync.Mutex
	down  bool
	tasks []*asynq.Task
}

func (q *dispatchQueue) Enqueue(t *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.down {
		return nil, errors.New("queue offline")
	}
	q.tasks = append(q.tasks, t)
	return &asynq.TaskInfo{ID: "accepted"}, nil
}
func (q *dispatchQueue) setDown(d bool) { q.mu.Lock(); defer q.mu.Unlock(); q.down = d }
func (q *dispatchQueue) snapshot() []*asynq.Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]*asynq.Task(nil), q.tasks...)
}

func TestFeishuDispatchSQLite(t *testing.T)   { feishuDispatchSuite(t, "sqlite") }
func TestFeishuDispatchPostgres(t *testing.T) { feishuDispatchSuite(t, "postgres") }

func feishuDispatchSuite(t *testing.T, dialect string) {
	db := syncdb.Open(t, dialect)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.TaskPendingOp{}, &types.Chunk{}))
	ctx := newCreateKnowledgeFileContext()
	fixture := func() (*knowledgeService,
		*dispatchFaultRepo,
		*dispatchQueue,
		*createKnowledgeFileServiceStub,
		*types.Knowledge,
	) {
		for _, model := range []any{&types.TaskPendingOp{}, &types.Chunk{}, &types.Knowledge{}} {
			require.NoError(t, db.Unscoped().Where("1 = 1").Delete(model).Error)
		}
		base := repository.NewKnowledgeRepository(db)
		repo := &dispatchFaultRepo{
			KnowledgeRepository:         base,
			KnowledgeDispatchRepository: base.(interfaces.KnowledgeDispatchRepository),
		}
		q := &dispatchQueue{}
		fs := &createKnowledgeFileServiceStub{}
		svc := &knowledgeService{
			repo:      repo,
			task:      q,
			fileSvc:   fs,
			chunkRepo: repository.NewChunkRepository(db),
			kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1}},
		}
		k := replayRow(types.ParseStatusPending)
		k.ID = uuid.NewString()
		k.TenantID = 1
		k.KnowledgeBaseID = "kb"
		k.FilePath = "stored/" + k.ID
		k.FileType = "docx"
		return svc, repo, q, fs, k
	}
	count := func(id string) {
		var n int64
		require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", id).Count(&n).Error)
		require.EqualValues(t, 1, n)
	}
	assertQueued := func(q *dispatchQueue, id string) {
		tasks := q.snapshot()
		require.NotEmpty(t, tasks)
		for _, task := range tasks {
			var p types.DocumentProcessPayload
			require.NoError(t, json.Unmarshal(task.Payload(), &p))
			require.Equal(t, id, p.KnowledgeID)
			require.Equal(t, "document-"+id, p.DispatchID)
			require.Positive(t, p.Attempt)
		}
	}
	t.Run("enqueue_failure_restart_recovers_original_id", func(t *testing.T) {
		s, r, q, fs, k := fixture()
		q.setDown(true)
		got, err := s.acceptFeishuKnowledgeDispatch(ctx, k, fs, nil)
		require.Error(t, err)
		require.Equal(t, k.ID, got.ID)
		op, err := r.GetKnowledgeDispatch(ctx, 1, k.ID)
		require.NoError(t, err)
		require.Equal(t, "pending", op.Op)
		q.setDown(false)
		restarted := restartKnowledgeServiceForTest(t, s)
		require.NoError(t, restarted.RecoverKnowledgeDispatches(ctx))
		assertQueued(q, k.ID)
		count(k.ID)
		require.Zero(t, fs.deleteCalls)
		// Actual delivery reaches the original DB row, then recognizes completed
		// state without touching source/chunks. This is queue, not index evidence.
		require.NoError(
			t,
			db.Model(&types.Knowledge{}).
				Where("id = ?", k.ID).
				UpdateColumn("parse_status", types.ParseStatusCompleted).
				Error,
		)
		require.NoError(t, restarted.ProcessDocument(ctx, q.snapshot()[len(q.snapshot())-1]))
		op, err = r.GetKnowledgeDispatch(ctx, 1, k.ID)
		require.NoError(t, err)
		require.Equal(t, "completed", op.Op)
	})
	t.Run("enqueue_success_ack_failure_replay", func(t *testing.T) {
		s, r, q, fs, k := fixture()
		r.ackFailure = true
		_, err := s.acceptFeishuKnowledgeDispatch(ctx, k, fs, nil)
		require.Error(t, err)
		assertQueued(q, k.ID)
		r.ackFailure = false
		require.NoError(t, s.ResumeKnowledgeDispatch(ctx, k))
		assertQueued(q, k.ID)
		count(k.ID)
		require.Zero(t, fs.deleteCalls)
	})
	t.Run("unknown_commit_reconciles_same_id", func(t *testing.T) {
		s, r, q, fs, k := fixture()
		r.commitUnknown = true
		got, err := s.acceptFeishuKnowledgeDispatch(ctx, k, fs, nil)
		require.NoError(t, err)
		require.Equal(t, k.ID, got.ID)
		count(k.ID)
		assertQueued(q, k.ID)
		require.Zero(t, fs.deleteCalls)
	})
	t.Run("unknown_commit_and_read_retains_blob_and_intent", func(t *testing.T) {
		s, r, q, fs, k := fixture()
		r.commitUnknown = true
		r.readFailure = errors.New("reconciliation read unavailable")
		got, err := s.acceptFeishuKnowledgeDispatch(ctx, k, fs, nil)
		require.Error(t, err)
		require.Equal(t, k.ID, got.ID)
		require.Zero(t, fs.deleteCalls)
		require.Empty(t, q.snapshot())
		r.readFailure = nil
		r.commitUnknown = false
		count(k.ID)
		require.NoError(t, s.ResumeKnowledgeDispatch(ctx, k))
		assertQueued(q, k.ID)
	})
	t.Run("unknown_commit_not_found_is_not_proof_for_blob_deletion", func(t *testing.T) {
		s, r, q, fs, k := fixture()
		r.commitUnknown = true
		r.readFailure = repository.ErrKnowledgeNotFound
		_, err := s.acceptFeishuKnowledgeDispatch(ctx, k, fs, nil)
		require.Error(t, err)
		require.Zero(t, fs.deleteCalls)
		require.Empty(t, q.snapshot())
		r.readFailure = nil
		count(k.ID)
		require.NoError(t, s.ResumeKnowledgeDispatch(ctx, k))
		assertQueued(q, k.ID)
	})
	t.Run("exit_between_accept_and_publish", func(t *testing.T) {
		s, r, q, _, k := fixture()
		op, err := s.dispatchIntent(ctx, k)
		require.NoError(t, err)
		require.NoError(t, r.AcceptKnowledgeDispatch(ctx, k, op))
		require.Empty(t, q.snapshot())
		restart := restartKnowledgeServiceForTest(t, s)
		require.NoError(t, restart.RecoverKnowledgeDispatches(ctx))
		assertQueued(q, k.ID)
		count(k.ID)
	})
	t.Run("duplicate_deliveries_skip_completed_content", func(t *testing.T) {
		s, _, q, fs, k := fixture()
		_, err := s.acceptFeishuKnowledgeDispatch(ctx, k, fs, nil)
		require.NoError(t, err)
		require.NoError(
			t,
			db.Model(&types.Knowledge{}).
				Where("id = ?", k.ID).
				UpdateColumn("parse_status", types.ParseStatusCompleted).
				Error,
		)
		for i := 0; i < 2; i++ {
			require.NoError(t, s.ProcessDocument(ctx, q.snapshot()[0]))
		}
		count(k.ID)
		require.Zero(t, fs.deleteCalls)
	})
	t.Run("concurrent_recovery_and_workers_preserve_content", func(t *testing.T) {
		s, _, q, fs, k := fixture()
		_, err := s.acceptFeishuKnowledgeDispatch(ctx, k, fs, nil)
		require.NoError(t, err)
		require.NoError(
			t,
			db.Model(&types.Knowledge{}).
				Where("id = ?", k.ID).
				UpdateColumn("parse_status", types.ParseStatusCompleted).
				Error,
		)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = s.ResumeKnowledgeDispatch(ctx, k)
				_ = s.ProcessDocument(ctx, q.snapshot()[0])
			}()
		}
		wg.Wait()
		require.NoError(t, s.ProcessDocument(ctx, q.snapshot()[0]))
		count(k.ID)
		require.Zero(t, fs.deleteCalls)
	})
	t.Run("expired_attempt_does_not_write_knowledge", func(t *testing.T) {
		s, r, q, fs, k := fixture()
		_, err := s.acceptFeishuKnowledgeDispatch(ctx, k, fs, nil)
		require.NoError(t, err)
		s.spanTracker = newAttemptTracker(2)
		require.NoError(t, s.ProcessDocument(ctx, q.snapshot()[0]))
		op, err := r.GetKnowledgeDispatch(ctx, 1, k.ID)
		require.NoError(t, err)
		require.Equal(t, "obsolete", op.Op)
		current, err := r.GetKnowledgeByID(ctx, 1, k.ID)
		require.NoError(t, err)
		require.Equal(t, types.ParseStatusPending, current.ParseStatus)
		count(k.ID)
		require.Zero(t, fs.deleteCalls)
	})
	t.Run("same_source_revision_fetch_resumes", func(t *testing.T) {
		s, _, q, fs, k := fixture()
		q.setDown(true)
		_, err := s.acceptFeishuKnowledgeDispatch(ctx, k, fs, nil)
		require.Error(t, err)
		item := &types.FetchedItem{Content: []byte("v1"), FileName: k.FileName}
		require.Error(t, reconcileFeishuReplay(ctx, s, k, item))
		q.setDown(false)
		var duplicate *types.DuplicateKnowledgeError
		require.ErrorAs(t, reconcileFeishuReplay(ctx, s, k, item), &duplicate)
		require.Equal(t, k.ID, duplicate.Knowledge.ID)
		assertQueued(q, k.ID)
		count(k.ID)
		require.Zero(t, fs.deleteCalls)
	})
	t.Run("file_saved_db_rollback_cleanup_only_candidate", func(t *testing.T) {
		s, r, q, fs, k := fixture()
		r.rollback = true
		_, err := s.acceptFeishuKnowledgeDispatch(ctx, k, fs, nil)
		require.Error(t, err)
		require.Equal(t, 1, fs.deleteCalls)
		require.Equal(t, k.FilePath, fs.deletedPath)
		require.Empty(t, q.snapshot())
		var n int64
		require.NoError(t, db.Model(&types.Knowledge{}).Where("id = ?", k.ID).Count(&n).Error)
		require.Zero(t, n)
	})
	t.Run("partial_chunks_are_never_erased_by_recovery", func(t *testing.T) {
		s, r, q, fs, k := fixture()
		_, err := s.acceptFeishuKnowledgeDispatch(ctx, k, fs, nil)
		require.NoError(t, err)
		chunk := &types.Chunk{
			ID:              uuid.NewString(),
			TenantID:        1,
			KnowledgeID:     k.ID,
			KnowledgeBaseID: "kb",
			Content:         "preserve",
		}
		require.NoError(t, db.Create(chunk).Error)
		s.chunkRepo = repository.NewChunkRepository(db)
		require.ErrorIs(t, s.ProcessDocument(ctx, q.snapshot()[0]), ErrFeishuDispatchUnconfirmed)
		op, err := r.GetKnowledgeDispatch(ctx, 1, k.ID)
		require.NoError(t, err)
		require.Equal(t, "attention", op.Op)
		var existing types.Chunk
		require.NoError(t, db.First(&existing, "id = ?", chunk.ID).Error)
		require.Equal(t, "preserve", existing.Content)
		require.Zero(t, fs.deleteCalls)
		count(k.ID)
	})
	t.Run("atomic_transaction_rolls_back_both", func(t *testing.T) {
		s, r, _, _, k := fixture()
		op, err := s.dispatchIntent(ctx, k)
		require.NoError(t, err)
		op.ID = 9223372036854775000
		require.NoError(t, db.Create(op).Error)
		// Colliding intent PK fails the second statement after Knowledge insert.
		require.Error(t, r.AcceptKnowledgeDispatch(ctx, k, op))
		_, err = r.GetKnowledgeByID(ctx, 1, k.ID)
		require.ErrorIs(t, err, repository.ErrKnowledgeNotFound)
		require.NoError(t, db.Delete(op).Error)
	})
	t.Run("stable_identity_scoped_by_source_and_revision", func(t *testing.T) {
		m := map[string]string{"datasource_id": "a", "external_id": "b"}
		id := feishuAdmissionID(1, "kb", m, "v1", "doc")
		require.Equal(t, id, feishuAdmissionID(1, "kb", m, "v1", "doc"))
		require.Equal(
			t,
			id,
			feishuAdmissionID(1, "kb", m, "v2", "doc"),
			"revision changes must not create another logical Knowledge",
		)
		require.NotEqual(t, id, feishuAdmissionID(2, "kb", m, "v1", "doc"))
	})
	t.Run("concurrent_initial_revisions_cannot_create_k2", func(t *testing.T) {
		s, r, q, fs, k := fixture()
		metadata := map[string]string{"datasource_id": "source", "external_id": "doc"}
		k.ID = feishuAdmissionID(1, "kb", metadata, "v1", k.FileName)
		other := *k
		other.FileHash = "changed"
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, candidate := range []*types.Knowledge{k, &other} {
			candidate := candidate
			wg.Add(1)
			go func() {
				defer wg.Done()
				op, err := s.dispatchIntent(ctx, candidate)
				if err == nil {
					err = r.AcceptKnowledgeDispatch(ctx, candidate, op)
				}
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		successes := 0
		for err := range errs {
			if err == nil {
				successes++
			}
		}
		require.Equal(t, 1, successes)
		var rows, intents int64
		require.NoError(t, db.Model(&types.Knowledge{}).Count(&rows).Error)
		require.EqualValues(t, 1, rows)
		require.NoError(t, db.Model(&types.TaskPendingOp{}).Count(&intents).Error)
		require.EqualValues(t, 1, intents)
		stored, err := r.GetKnowledgeByID(ctx, 1, k.ID)
		require.NoError(t, err)
		require.NoError(t, s.ResumeKnowledgeDispatch(ctx, stored))
		assertQueued(q, k.ID)
		require.Zero(t, fs.deleteCalls)
	})
	t.Run("full_create_failure_retry_and_stream_cursor", func(t *testing.T) {
		s, _, q, fs, _ := fixture()
		q.setDown(true)
		metadata := map[string]string{"datasource_id": "source", "external_id": "doc"}
		k, err := s.CreateKnowledgeFromFile(
			ctx,
			"kb",
			newMultipartFileHeader(t, "doc.txt", "v1"),
			metadata,
			nil,
			"",
			nil,
			"feishu",
			nil,
		)
		require.Error(t, err)
		require.NotNil(t, k)
		count(k.ID)
		require.Zero(t, fs.deleteCalls)
		ds := &types.DataSource{
			ID:              "source",
			TenantID:        1,
			KnowledgeBaseID: "kb",
			Type:            "feishu",
			LastSyncCursor:  types.JSON(`{"accepted":"previous"}`),
		}
		h := newStreamHandler(
			&DataSourceService{knowledgeService: s},
			ds,
			&types.SyncResult{},
			&types.SyncLog{},
		)
		item := types.FetchedItem{ExternalID: "doc", Content: []byte("v1"), FileName: "doc.txt"}
		require.Error(t, h.Emit(ctx, item))
		require.Error(t, h.Checkpoint(ctx, &types.SyncCursor{}))
		require.JSONEq(t, `{"accepted":"previous"}`, string(ds.LastSyncCursor))
		q.setDown(false)
		retried, err := s.CreateKnowledgeFromFile(
			ctx,
			"kb",
			newMultipartFileHeader(t, "doc.txt", "v1"),
			metadata,
			nil,
			"",
			nil,
			"feishu",
			nil,
		)
		var duplicate *types.DuplicateKnowledgeError
		require.ErrorAs(t, err, &duplicate)
		require.Equal(t, k.ID, retried.ID)
		count(k.ID)
		assertQueued(q, k.ID)
		require.Equal(t, 1, fs.saveCalls)
		fresh := newStreamHandler(
			&DataSourceService{knowledgeService: s},
			ds,
			&types.SyncResult{},
			&types.SyncLog{},
		)
		require.NoError(t, fresh.Emit(ctx, item))
		require.Equal(t, 1, fresh.result.Skipped)
		require.Zero(t, fs.deleteCalls)
	})
	t.Run("personal_first_create_recovery_is_user_scoped", func(t *testing.T) {
		s, _, q, fs, _ := fixture()
		q.setDown(true)
		metadata := map[string]string{"feishu_imported_by": "user-a", "feishu_resource_id": "docx:private"}
		k, err := s.CreateKnowledgeFromFile(
			ctx,
			"kb",
			newMultipartFileHeader(t, "doc.txt", "v1"),
			metadata,
			nil,
			"",
			nil,
			"feishu_personal",
			nil,
		)
		require.Error(t, err)
		require.NotNil(t, k)
		count(k.ID)
		q.setDown(false)
		restored, err := s.CreateKnowledgeFromFile(
			ctx,
			"kb",
			newMultipartFileHeader(t, "doc.txt", "v1"),
			metadata,
			nil,
			"",
			nil,
			"feishu_personal",
			nil,
		)
		require.NoError(
			t,
			err,
			"the personal caller can bind its existing import record after initial "+
				"response failure",
		)
		require.Equal(t, k.ID, restored.ID)
		assertQueued(q, k.ID)
		require.Equal(t, 1, fs.saveCalls)
		other := map[string]string{"feishu_imported_by": "user-b", "feishu_resource_id": "docx:private"}
		b, err := s.CreateKnowledgeFromFile(
			ctx,
			"kb",
			newMultipartFileHeader(t, "doc.txt", "v1"),
			other,
			nil,
			"",
			nil,
			"feishu_personal",
			nil,
		)
		require.NoError(t, err)
		require.NotEqual(t, k.ID, b.ID)
		var rows int64
		require.NoError(t, db.Model(&types.Knowledge{}).Count(&rows).Error)
		require.EqualValues(t, 2, rows)
		require.Zero(t, fs.deleteCalls)
	})
	t.Run("legacy_personal_failed_enqueue_recovers_old_random_id", func(t *testing.T) {
		s, r, q, fs, k := fixture()
		k.Channel = "feishu_personal"
		k.FileName = "doc.txt"
		k.FileType = "txt"
		k.ParseStatus = types.ParseStatusFailed
		k.ErrorMessage = "Failed to enqueue processing task"
		metadata := map[string]string{"feishu_imported_by": "user-a", "feishu_resource_id": "docx:private"}
		k.Metadata, _ = json.Marshal(metadata)
		require.NoError(t, db.Create(k).Error)
		got, err := s.CreateKnowledgeFromFile(
			ctx,
			"kb",
			newMultipartFileHeader(t, "doc.txt", "v1"),
			metadata,
			nil,
			"",
			nil,
			"feishu_personal",
			nil,
		)
		require.NoError(t, err)
		require.Equal(t, k.ID, got.ID)
		count(k.ID)
		assertQueued(q, k.ID)
		require.Zero(t, fs.saveCalls)
		require.Zero(t, fs.deleteCalls)
		op, err := r.GetKnowledgeDispatch(ctx, 1, k.ID)
		require.NoError(t, err)
		require.Equal(t, "dispatched", op.Op)
	})
	t.Run("changed_personal_replace_is_rejected_before_file_or_chunk_mutation", func(t *testing.T) {
		s, _, q, fs, k := fixture()
		k.Type = "file"
		k.Channel = "feishu_personal"
		k.FileName = "doc.txt"
		k.FileType = "txt"
		k.ParseStatus = types.ParseStatusCompleted
		require.NoError(t, db.Create(k).Error)
		grant, err := access.WithKBTaskWrite(ctx, &types.KnowledgeBase{ID: "kb", TenantID: 1}, 1)
		require.NoError(t, err)
		_, err = s.ReplaceKnowledgeFile(
			grant,
			k.ID,
			newMultipartFileHeader(t, "doc.txt", "v2"),
			"doc.txt",
			nil,
		)
		require.ErrorIs(t, err, ErrFeishuStagingRequired)
		require.Zero(t, fs.saveCalls)
		require.Zero(t, fs.deleteCalls)
		require.Empty(t, q.snapshot())
		count(k.ID)
	})
	if dialect == "postgres" {
		t.Run("recovery_database_unavailable_does_not_lose_intent", func(t *testing.T) {
			s, _, q, fs, k := fixture()
			q.setDown(true)
			_, err := s.acceptFeishuKnowledgeDispatch(ctx, k, fs, nil)
			require.Error(t, err)
			dead, err := gorm.Open(postgres.Open(db.Dialector.(*postgres.Dialector).DSN), &gorm.Config{})
			require.NoError(t, err)
			pool, err := dead.DB()
			require.NoError(t, err)
			require.NoError(t, pool.Close())
			restarting := restartKnowledgeServiceForTest(t, s)
			restarting.repo = repository.NewKnowledgeRepository(dead)
			require.Error(t, restarting.RecoverKnowledgeDispatches(ctx))
			require.Empty(t, q.snapshot())
			count(k.ID)
			require.Zero(t, fs.deleteCalls)
			q.setDown(false)
			require.NoError(t, s.RecoverKnowledgeDispatches(ctx))
			assertQueued(q, k.ID)
		})
	}
	t.Run("database_worker_lock_is_exclusive_without_expiry", func(t *testing.T) {
		_, r, _, _, k := fixture()
		entered := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- r.WithKnowledgeDispatchLock(
				ctx,

				k.ID,

				func(
					context.Context,
				) error {
					close(
						entered,
					)
					<-release
					return nil
				},
			)
		}()
		<-entered
		if dialect == "postgres" {
			require.Error(
				t,
				r.WithKnowledgeDispatchLock(
					ctx,
					k.ID,
					func(context.Context) error { t.Error("concurrent owner entered"); return nil },
				),
			)
		}
		close(release)
		require.NoError(t, <-done)
		require.NoError(
			t,
			r.WithKnowledgeDispatchLock(ctx, k.ID, func(context.Context) error { return nil }),
		)
	})
	if dialect == "postgres" {
		t.Run("lost_postgres_session_cancels_old_worker", func(t *testing.T) {
			_, r, _, _, k := fixture()
			entered := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- r.WithKnowledgeDispatchLock(ctx, k.ID, func(worker context.Context) error {
					close(entered)
					<-worker.Done()
					return db.WithContext(worker).Create(&types.Knowledge{ID: k.ID}).Error
				})
			}()
			<-entered
			// Only sessions holding this test's advisory lock are terminated.
			hash := fnv.New64a()
			_, _ = hash.Write([]byte("knowledge-dispatch:" + k.ID))
			key := hash.Sum64()
			require.NoError(
				t,
				db.Exec(
					"SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype = "+
						"'advisory' AND classid::bigint = ? AND objid::bigint = ? AND pid <> "+
						"pg_backend_pid() AND database = (SELECT oid FROM pg_database WHERE "+
						"datname = current_database())",
					int64(uint32(key>>32)),
					int64(uint32(key)),
				).Error,
			)
			select {
			case err := <-done:
				require.Error(t, err)
			case <-time.After(8 * time.Second):
				t.Fatal("lost lock did not cancel worker")
			}
			_, err := r.GetKnowledgeByID(ctx, 1, k.ID)
			require.ErrorIs(t, err, repository.ErrKnowledgeNotFound)
		})
	}
}

var _ interfaces.KnowledgeDispatchRepository = (*dispatchFaultRepo)(nil)
