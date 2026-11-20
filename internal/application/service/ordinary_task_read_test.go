package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func ordinaryTaskReadFixture(t *testing.T) (*gorm.DB, *FeishuPersonalService, *asynq.Task) {
	t.Helper()
	f := fixture.New(t)
	db := f.DB
	require.NoError(t, db.Create(&types.Knowledge{
		ID: "upload", TenantID: 99, KnowledgeBaseID: "public-kb", Channel: "web", Type: "file",
		FilePath: "ordinary-upload", FileHash: "ordinary-hash", ParseStatus: "pending", Metadata: types.JSON(`{}`),
	}).Error)
	require.NoError(t, db.Create(&types.Knowledge{
		ID: "held-source", TenantID: 99, KnowledgeBaseID: "public-kb", Channel: "feishu", Type: "file",
		FilePath: "restricted-original", ParseStatus: "completed", Metadata: types.JSON(`{}`),
	}).Error)
	require.NoError(t, db.Create(&types.DataSource{
		ID: "source", TenantID: 99, KnowledgeBaseID: "public-kb", Type: "feishu", Status: "error",
		LegacyAccessState: "active", Config: types.JSON(`{}`),
	}).Error)
	require.NoError(t, db.Create(&types.FeishuLegacyResource{
		ID: "held-resource", TenantID: 99, KnowledgeBaseID: "public-kb", DataSourceID: "source",
		KnowledgeID: "held-source", ExternalID: "held-external", State: "hold",
		QuarantineSnapshot: json.RawMessage(`{}`),
	}).Error)
	require.NoError(t, db.Exec("UPDATE knowledge_bases SET description=? WHERE id=?",
		"RESTRICTED_KB_PROFILE", "public-kb").Error)
	db = types.EnableLegacyReadGuards(types.EnablePrivateKBGuards(db))
	body, err := json.Marshal(types.DocumentProcessPayload{
		TenantID: 99, KnowledgeBaseID: "public-kb", KnowledgeID: "upload", FilePath: "ordinary-upload", Attempt: 1,
	})
	require.NoError(t, err)
	return db, &FeishuPersonalService{db: db}, asynq.NewTask(types.TypeDocumentProcess, body)
}

func TestOrdinaryTaskReadIsIndependentOfSiblingHoldAndNeverGrantsKBBody(t *testing.T) {
	db, s, task := ordinaryTaskReadFixture(t)
	repo := repository.NewKnowledgeRepository(db)
	_, err := repo.GetKnowledgeByID(context.Background(), 99, "upload")
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted, "the original unscoped worker is blocked")
	base := types.WithLegacyReadScope(context.Background())
	err = s.WrapPrivateTaskHandler(func(ctx context.Context, _ *asynq.Task) error {
		row, err := repo.GetKnowledgeByID(ctx, 99, "upload")
		require.NoError(t, err)
		require.Equal(t, "upload", row.ID)
		require.Equal(t, types.CallerFromContext(base), types.CallerFromContext(ctx), "no human or admin is invented")
		_, err = repo.GetKnowledgeByID(ctx, 99, "held-source")
		require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
		_, err = repo.GetKnowledgeByID(ctx, 99, "public-doc")
		require.ErrorIs(t, err, types.ErrLegacySourceRestricted, "the task cannot read a sibling")
		kb, err := repository.NewKnowledgeBaseRepository(db).GetKnowledgeBaseByID(ctx, "public-kb")
		require.NoError(t, err)
		require.Equal(t, "public-kb", kb.ID)
		require.EqualValues(t, 99, kb.TenantID)
		require.Empty(t, kb.Description)
		require.Nil(t, kb.GeneratedProfile)
		var full types.KnowledgeBase
		require.ErrorIs(t, db.WithContext(ctx).First(&full, "id=?", "public-kb").Error,
			types.ErrLegacySourceRestricted, "only the repository configuration projection is allowed")
		require.NoError(t, types.CheckLegacyReadScope(db.WithContext(ctx)), "model inputs recheck this document only")
		return nil
	})(base, task)
	require.NoError(t, err)
	_, err = repo.GetKnowledgeByID(base, 99, "upload")
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted, "the capability does not escape the worker")
}

func TestOrdinaryTaskReadRevalidatesSourceReplacementAndProtection(t *testing.T) {
	for _, scenario := range []string{
		"file", "channel", "metadata", "legacy", "personal", "receipt", "private", "tenant", "deleted",
	} {
		t.Run(scenario, func(t *testing.T) {
			db, s, task := ordinaryTaskReadFixture(t)
			err := s.WrapPrivateTaskHandler(func(ctx context.Context, _ *asynq.Task) error {
				repo := repository.NewKnowledgeRepository(db)
				_, err := repo.GetKnowledgeByID(ctx, 99, "upload")
				require.NoError(t, err)
				switch scenario {
				case "file":
					require.NoError(t, db.Exec("UPDATE knowledges SET file_path='replacement' WHERE id='upload'").Error)
				case "channel":
					require.NoError(
						t,
						db.Exec("UPDATE knowledges SET channel='feishu_personal' WHERE id='upload'").Error,
					)
				case "metadata":
					require.NoError(t, db.Exec("UPDATE knowledges SET metadata=? WHERE id='upload'",
						`{"datasource_id":"source"}`).Error)
				case "legacy":
					require.NoError(t, db.Create(&types.FeishuLegacyResource{
						ID: "new-binding", TenantID: 99, KnowledgeBaseID: "public-kb", DataSourceID: "source",
						KnowledgeID: "upload", ExternalID: "new-external", State: "active",
						QuarantineSnapshot: json.RawMessage(`{}`),
					}).Error)
				case "personal":
					require.NoError(t, db.Create(&types.FeishuPersonalResource{
						ImportID: "new-personal", LogicalKnowledgeID: "upload",
					}).Error)
				case "receipt":
					require.NoError(t, db.Create(&types.TaskPendingOp{
						TenantID: 99, Scope: types.PersonalExactReceiptScope, ScopeID: "upload", Op: "staging",
						Payload: json.RawMessage(`{}`),
					}).Error)
				case "private":
					require.NoError(
						t,
						db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('public-kb','A')").Error,
					)
				case "tenant":
					require.NoError(t, db.Exec("UPDATE tenants SET status='inactive' WHERE id=99").Error)
				case "deleted":
					require.NoError(
						t,
						db.Exec("UPDATE knowledge_bases SET deleted_at=CURRENT_TIMESTAMP WHERE id='public-kb'").Error,
					)
				}
				_, err = repo.GetKnowledgeByID(ctx, 99, "upload")
				require.Error(t, err, "an old context must not retain authority after a live binding changes")
				_, err = repository.NewKnowledgeBaseRepository(db).GetKnowledgeBaseByID(ctx, "public-kb")
				require.Error(t, err)
				return nil
			})(types.WithLegacyReadScope(context.Background()), task)
			require.NoError(t, err)
		})
	}
}

func TestOrdinaryTaskReadAllowsDerivedImageProgressButRejectsNewParseEpoch(t *testing.T) {
	db, s, task := ordinaryTaskReadFixture(t)
	addAttempt := func(attempt int) {
		require.NoError(t, db.Create(&types.KnowledgeProcessingSpan{
			KnowledgeID: "upload", Attempt: attempt, SpanID: "parse-epoch", Name: "parse",
			Kind: "stage", Status: "running",
		}).Error)
	}
	var first context.Context
	err := s.WrapPrivateTaskHandler(func(ctx context.Context, _ *asynq.Task) error {
		first = ctx
		repo := repository.NewKnowledgeRepository(db)
		addAttempt(1)
		require.NoError(t, db.Exec("UPDATE knowledges SET file_hash='derived-image-progress' WHERE id='upload'").Error)
		_, err := repo.GetKnowledgeByID(ctx, 99, "upload")
		require.NoError(t, err, "image annotations do not replace the immutable original")
		addAttempt(2)
		_, err = repo.GetKnowledgeByID(ctx, 99, "upload")
		require.ErrorIs(t, err, types.ErrLegacySourceRestricted, "reparse invalidates the old task scope")
		return nil
	})(context.Background(), task)
	require.NoError(t, err)
	require.NotNil(t, first)
	called := false
	err = s.WrapPrivateTaskHandler(func(context.Context, *asynq.Task) error { called = true; return nil })(
		context.Background(), task,
	)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted, "a stale delivery cannot mint a new scope")
	require.ErrorIs(t, err, asynq.SkipRetry, "stale deliveries do not spend another retry budget")
	require.False(t, called)
	var payload types.DocumentProcessPayload
	require.NoError(t, json.Unmarshal(task.Payload(), &payload))
	payload.Attempt = 2
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	err = s.WrapPrivateTaskHandler(func(ctx context.Context, _ *asynq.Task) error {
		_, err := repository.NewKnowledgeRepository(db).GetKnowledgeByID(ctx, 99, "upload")
		return err
	})(context.Background(), asynq.NewTask(task.Type(), body))
	require.NoError(t, err, "the new admitted attempt has its own bounded read scope")
}
