package service

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWikiOwnershipErrorClassificationAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		err   error
		class string
	}{{context.Canceled, "context_canceled"}, {context.DeadlineExceeded, "deadline_exceeded"}, {gorm.ErrRecordNotFound, "not_found"}, {repository.ErrKnowledgeNotFound, "not_found"}, {driver.ErrBadConn, "database_connection"}, {&pgconn.PgError{Code: "08006", Message: "secret SQL body"}, "database_connection"}, {errors.New("secret token SQL body"), "query_error"}} {
		t.Run(tc.class, func(t *testing.T) {
			cause := fmt.Errorf("secret context: %w", tc.err)
			err := wikiOwnershipFailure(context.Background(), "secret-kb", WikiPendingOp{dbID: 42, KnowledgeID: "secret-id"}, cause)
			var diagnosed *wikiOwnershipReadError
			require.ErrorAs(t, err, &diagnosed)
			require.ErrorIs(t, diagnosed.Cause(), tc.err)
			require.Contains(t, err.Error(), tc.class)
			require.NotContains(t, err.Error(), "secret")
			require.Equal(t, "invalid", wikiDiagnosticID("secret"))
		})
	}
}

type ownershipReadKnowledge struct {
	interfaces.KnowledgeService
	db    *gorm.DB
	cause error
}

func (k *ownershipReadKnowledge) GetKnowledgeByIDOnly(ctx context.Context, id string) (*types.Knowledge, error) {
	var row types.Knowledge
	if err := k.db.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		return nil, err
	}
	if k.cause != nil {
		return nil, k.cause
	}
	return &row, nil
}

func TestWikiOwnershipSQLFailurePreservesOperationAndFinalizingSlot(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := syncdb.Open(t, dialect)
			require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}))
			require.NoError(t, db.Exec("CREATE TABLE knowledges(id text PRIMARY KEY,channel text,metadata text,parse_status text,pending_subtasks_count integer,deleted_at timestamp)").Error)
			require.NoError(t, db.Exec("INSERT INTO knowledges VALUES('doc','web','{}','finalizing',1,NULL)").Error)
			stamp := time.Now().UTC().Truncate(time.Microsecond)
			op := types.TaskPendingOp{TenantID: 1, Scope: "knowledge_base", ScopeID: "kb", TaskType: "wiki:ingest", Op: "ingest", DedupKey: "doc", Payload: []byte(`{"knowledge_id":"doc","attempt":1}`), ClaimedAt: &stamp}
			require.NoError(t, db.Create(&op).Error)
			s := &wikiIngestService{knowledgeSvc: &ownershipReadKnowledge{db: db, cause: driver.ErrBadConn}}
			first, err := s.firstAddOwnership(context.Background(), "kb", WikiPendingOp{KnowledgeID: "doc", dbID: op.ID})
			require.False(t, first)
			var diagnosed *wikiOwnershipReadError
			require.ErrorAs(t, err, &diagnosed)
			require.ErrorIs(t, diagnosed.Cause(), driver.ErrBadConn)
			require.Contains(t, err.Error(), "database_connection")
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = s.firstAddOwnership(canceled, "kb", WikiPendingOp{KnowledgeID: "doc", dbID: op.ID})
			require.ErrorAs(t, err, &diagnosed)
			require.ErrorIs(t, diagnosed.Cause(), context.Canceled)
			_, err = s.firstAddOwnership(context.Background(), "kb", WikiPendingOp{KnowledgeID: "missing", dbID: op.ID})
			require.Contains(t, err.Error(), "not_found")
			var saved types.TaskPendingOp
			require.NoError(t, db.First(&saved, op.ID).Error)
			require.Equal(t, "knowledge_base", saved.Scope)
			require.Equal(t, "ingest", saved.Op)
			require.True(t, saved.ClaimedAt.Equal(stamp))
			require.Equal(t, op.FailCount, saved.FailCount)
			require.JSONEq(t, string(op.Payload), string(saved.Payload))
			var row struct {
				ParseStatus          string
				PendingSubtasksCount int
			}
			require.NoError(t, db.Table("knowledges").Where("id='doc'").Scan(&row).Error)
			require.Equal(t, "finalizing", row.ParseStatus)
			require.Equal(t, 1, row.PendingSubtasksCount)
		})
	}
}

func TestWikiOwnershipDiagnosticsDoNotChangeQueuePolicy(t *testing.T) {
	for _, cause := range []error{asynq.SkipRetry, types.ErrLegacySourceRestricted, repository.ErrWikiMCPUnverified} {
		err := wikiOwnershipFailure(context.Background(), "kb", WikiPendingOp{}, cause)
		require.NotErrorIs(t, err, cause, "an observational change must not expose a new queue-policy sentinel")
		svc := &wikiIngestService{}
		svc.recordWikiBlock(context.Background(), WikiIngestPayload{}, &err)
		require.Error(t, err)
	}
}
