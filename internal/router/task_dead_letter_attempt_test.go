package router

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type finalizedAttempt struct {
	knowledgeID string
	attempt     int
	status      string
}

func TestDeadLetterOrdinaryUploadInMixedKBDoesNotRemainPending(t *testing.T) {
	f := fixture.New(t)
	db := f.DB
	require.NoError(t, db.Create(&types.Knowledge{
		ID: "upload", TenantID: 99, KnowledgeBaseID: "public-kb", Channel: "web", Type: "file",
		FilePath: "ordinary-upload", FileHash: "ordinary-hash", ParseStatus: "pending", Metadata: types.JSON(`{}`),
	}).Error)
	require.NoError(t, db.Create(&types.DataSource{
		ID: "source", TenantID: 99, KnowledgeBaseID: "public-kb", Type: "feishu", Status: "error",
		LegacyAccessState: "hold", Config: types.JSON(`{}`),
	}).Error)
	db = types.EnableLegacyReadGuards(types.EnablePrivateKBGuards(db))
	repo := repository.NewKnowledgeRepository(db)
	callback := newDeadLetterKnowledgeFailer(transferFailureKnowledge{repo: repo}, nil)
	payload, err := json.Marshal(types.DocumentProcessPayload{
		TenantID: 99, KnowledgeID: "upload", KnowledgeBaseID: "public-kb", Attempt: 1,
	})
	require.NoError(t, err)
	task := asynq.NewTask(types.TypeDocumentProcess, payload)
	callback(context.Background(), task, errors.New("parser unavailable"))
	var status string
	require.NoError(t, db.Table("knowledges").Select("parse_status").Where("id='upload'").Scan(&status).Error)
	require.Equal(t, "pending", status, "the original failure callback cannot load this row")
	personal := service.NewFeishuPersonalService(db, nil, nil, nil, nil)
	withDocumentTaskFailureContext(callback, personal)(context.Background(), task, errors.New("parser unavailable"))
	var row struct{ ParseStatus, ErrorMessage string }
	require.NoError(
		t,
		db.Table("knowledges").Select("parse_status,error_message").Where("id='upload'").Scan(&row).Error,
	)
	require.Equal(t, "failed", row.ParseStatus)
	require.Contains(t, row.ErrorMessage, "parser unavailable")
	var sourceState string
	require.NoError(
		t,
		db.Table("data_sources").Select("legacy_access_state").Where("id='source'").Scan(&sourceState).Error,
	)
	require.Equal(t, "hold", sourceState, "status reporting cannot release source protection")
}

// attemptTracker is a SpanTracker stub: only the two methods the dead-letter
// callback uses are implemented; anything else panics via the nil embed.
type attemptTracker struct {
	service.SpanTracker
	latest int

	mu        sync.Mutex
	finalized []finalizedAttempt
}

func (a *attemptTracker) LatestAttempt(context.Context, string) int { return a.latest }

func (a *attemptTracker) FinalizeAttempt(
	_ context.Context, knowledgeID string, attempt int, status string, _ types.JSONMap, _, _ string,
) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.finalized = append(a.finalized, finalizedAttempt{knowledgeID: knowledgeID, attempt: attempt, status: status})
}

func TestDeadLetterCallbackRespectsSupersededAttempt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "attempt.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}))
	repo := repository.NewKnowledgeRepository(db)

	for _, tc := range []struct {
		id             string
		payloadAttempt int
		ordinaryID     int64
		latestAttempt  int
		wantFailed     bool
		wantFinalized  []finalizedAttempt
	}{
		{id: "superseded", payloadAttempt: 1, latestAttempt: 2, wantFailed: false},
		{id: "ordinary-window", payloadAttempt: 2, latestAttempt: 2, ordinaryID: 19, wantFailed: false},
		{
			id: "latest", payloadAttempt: 2, latestAttempt: 2, wantFailed: true,
			wantFinalized: []finalizedAttempt{{"latest", 2, types.SpanStatusFailed}},
		},
		// Legacy payloads without an attempt cannot be compared, so the row
		// is still failed (and no span is finalized).
		{id: "legacy", payloadAttempt: 0, latestAttempt: 3, wantFailed: true},
	} {
		t.Run(tc.id, func(t *testing.T) {
			require.NoError(t, db.Create(&types.Knowledge{
				ID: tc.id, TenantID: 7, KnowledgeBaseID: "kb", ParseStatus: types.ParseStatusProcessing,
			}).Error)
			tracker := &attemptTracker{latest: tc.latestAttempt}
			callback := newDeadLetterKnowledgeFailer(transferFailureKnowledge{repo: repo}, tracker)
			payload, err := json.Marshal(deadLetterKnowledgePayload{
				TenantID: 7, KnowledgeBaseID: "kb", KnowledgeID: tc.id, Attempt: tc.payloadAttempt,
				OrdinaryEmbeddingID: tc.ordinaryID,
			})
			require.NoError(t, err)

			callback(context.Background(), asynq.NewTask(types.TypeDocumentProcess, payload), errors.New("timeout"))

			row, err := repo.GetKnowledgeByID(context.Background(), 7, tc.id)
			require.NoError(t, err)
			if tc.wantFailed {
				require.Equal(t, types.ParseStatusFailed, row.ParseStatus)
				require.Contains(t, row.ErrorMessage, "exhausted retries: timeout")
			} else {
				require.Equal(t, types.ParseStatusProcessing, row.ParseStatus)
				require.Empty(t, row.ErrorMessage)
			}
			require.Equal(t, tc.wantFinalized, tracker.finalized)
		})
	}
}
