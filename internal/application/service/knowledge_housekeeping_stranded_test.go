package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func insertKnowledgeWithSummary(t *testing.T, db *gorm.DB, id, status, summary string, updatedAt time.Time) {
	t.Helper()
	require.NoError(t, db.Exec(
		`INSERT INTO knowledges (id, parse_status, summary_status, updated_at) VALUES (?, ?, ?, ?)`,
		id, status, summary, updatedAt,
	).Error)
}

func readSummaryStatus(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	var summary string
	require.NoError(t, db.Raw(`SELECT summary_status FROM knowledges WHERE id = ?`, id).Scan(&summary).Error)
	return summary
}

func countWikiOps(t *testing.T, db *gorm.DB, knowledgeID string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM task_pending_ops WHERE dedup_key = ?`, knowledgeID).
		Scan(&n).Error)
	return n
}

// A stuck row failed by the sweep has lost its summary task with the rest of
// the run; an unfinished summary must fail with it, a finished one is kept.
func TestHousekeepingRecoverStalledFailsUnfinishedSummary(t *testing.T) {
	db := setupHousekeepingDB(t)
	svc := newHousekeepingSvcForTest(db)
	stale := time.Now().Add(-3 * time.Hour)
	insertKnowledgeWithSummary(
		t,
		db,
		"k-pending",
		types.ParseStatusFinalizing,
		types.SummaryStatusPending,
		stale,
	)
	insertKnowledgeWithSummary(
		t,
		db,
		"k-running",
		types.ParseStatusFinalizing,
		types.SummaryStatusProcessing,
		stale,
	)
	insertKnowledgeWithSummary(
		t,
		db,
		"k-done",
		types.ParseStatusFinalizing,
		types.SummaryStatusCompleted,
		stale,
	)

	svc.runSweep(context.Background())

	for id, want := range map[string]string{
		"k-pending": types.SummaryStatusFailed,
		"k-running": types.SummaryStatusFailed,
		"k-done":    types.SummaryStatusCompleted,
	} {
		status, _ := readKnowledgeStatus(t, db, id)
		assert.Equal(t, types.ParseStatusFailed, status, id)
		assert.Equal(t, want, readSummaryStatus(t, db, id), id)
	}
}

// Only the summary task moves a summary out of "pending"; when it gives up
// without writing a status the row keeps a spinner forever. Finished rows
// with nothing queued are failed; a queued summary task, a run still in
// flight and a recent row are left alone.
func TestHousekeepingRecoversStrandedPendingSummaries(t *testing.T) {
	db := setupHousekeepingDB(t)
	svc := newHousekeepingSvcWithInspector(db, fakeTaskInspector{
		queued: map[string]bool{"k-queued": true},
	})
	stale := time.Now().Add(-2 * time.Hour)
	cutoff := time.Now().Add(-time.Hour)
	for _, status := range []string{types.ParseStatusCompleted, types.ParseStatusFailed, types.ParseStatusCancelled} {
		insertKnowledgeWithSummary(t, db, "k-"+status, status, types.SummaryStatusPending, stale)
	}
	insertKnowledgeWithSummary(t, db, "k-queued", types.ParseStatusCompleted, types.SummaryStatusPending, stale)
	insertKnowledgeWithSummary(
		t,
		db,
		"k-finalizing",
		types.ParseStatusFinalizing,
		types.SummaryStatusPending,
		stale,
	)
	insertKnowledgeWithSummary(
		t,
		db,
		"k-recent",
		types.ParseStatusCompleted,
		types.SummaryStatusPending,
		time.Now(),
	)

	svc.recoverStrandedPendingSummaries(context.Background(), cutoff)

	for id, want := range map[string]string{
		"k-" + types.ParseStatusCompleted: types.SummaryStatusFailed,
		"k-" + types.ParseStatusFailed:    types.SummaryStatusFailed,
		"k-" + types.ParseStatusCancelled: types.SummaryStatusFailed,
		"k-queued":                        types.SummaryStatusPending,
		"k-finalizing":                    types.SummaryStatusPending,
		"k-recent":                        types.SummaryStatusPending,
	} {
		assert.Equal(t, want, readSummaryStatus(t, db, id), id)
	}
}

func TestHousekeepingSummarySweepPreservesProtocolAndQueuedWork(t *testing.T) {
	for _, summary := range []string{types.SummaryStatusPending, types.SummaryStatusProcessing} {
		t.Run(summary, func(t *testing.T) {
			db := setupHousekeepingDB(t)
			inspector := fakeTaskInspector{queued: map[string]bool{"queued": true}}
			svc := newHousekeepingSvcWithInspector(db, inspector)
			stale := time.Now().Add(-2 * time.Hour)
			for _, id := range []string{"personal-live", "personal-stopped", "queued", "orphan"} {
				insertKnowledgeWithSummary(t, db, id, types.ParseStatusCompleted, summary, stale)
			}
			for _, id := range []string{"personal-live", "personal-stopped"} {
				require.NoError(t, db.Exec(`INSERT INTO task_pending_ops
(task_type,scope,scope_id,op) VALUES (?,?,?,?)`, types.TypeDocumentProcess,
					types.KnowledgeDispatchScope, id, "processing").Error)
			}
			svc.recoverOrphanedSummaries(context.Background(), time.Now().Add(-time.Hour), summary)
			for _, id := range []string{"personal-live", "personal-stopped", "queued"} {
				require.Equal(t, summary, readSummaryStatus(t, db, id), id)
			}
			require.Equal(t, types.SummaryStatusFailed, readSummaryStatus(t, db, "orphan"))
		})
	}
}

func TestHousekeepingSummarySweepDefersUnknownQueueState(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		db := setupHousekeepingDB(t)
		insertKnowledgeWithSummary(t, db, "unknown", types.ParseStatusCompleted,
			types.SummaryStatusProcessing, time.Now().Add(-2*time.Hour))
		svc := newHousekeepingSvcWithInspector(db, nil)
		if unavailable {
			svc.inspector = fakeTaskInspector{err: errors.New("redis unavailable")}
		}
		svc.recoverOrphanedSummaries(context.Background(), time.Now().Add(-time.Hour),
			types.SummaryStatusProcessing)
		require.Equal(t, types.SummaryStatusProcessing, readSummaryStatus(t, db, "unknown"))
	}
}

type summaryProbeInspector struct {
	fakeTaskInspector
	probe func()
}

func (s summaryProbeInspector) HasQueuedTasksForKnowledge(context.Context, string) (bool, error) {
	s.probe()
	return false, nil
}

func TestHousekeepingSummarySweepDoesNotOverwriteConcurrentProgress(t *testing.T) {
	for _, change := range []string{"new_progress", "new_protocol"} {
		t.Run(change, func(t *testing.T) {
			db := setupHousekeepingDB(t)
			insertKnowledgeWithSummary(t, db, "racing", types.ParseStatusCompleted,
				types.SummaryStatusProcessing, time.Now().Add(-2*time.Hour))
			svc := newHousekeepingSvcForTest(db)
			svc.inspector = summaryProbeInspector{probe: func() {
				if change == "new_progress" {
					require.NoError(t, db.Exec("UPDATE knowledges SET updated_at=? WHERE id=?",
						time.Now(), "racing").Error)
				} else {
					require.NoError(t, db.Exec(`INSERT INTO task_pending_ops
(task_type,scope,scope_id,op) VALUES (?,?,?,?)`, types.TypeDocumentProcess,
						types.KnowledgeDispatchScope, "racing", "processing").Error)
				}
			}}
			svc.recoverOrphanedSummaries(context.Background(), time.Now().Add(-time.Hour),
				types.SummaryStatusProcessing)
			require.Equal(t, types.SummaryStatusProcessing, readSummaryStatus(t, db, "racing"))
		})
	}
}

// A durable wiki op protects its row only up to wikiHoldLimit: a consumer that
// fails before claiming never consumes the op, and the row would otherwise sit
// in "finalizing" forever. Past the limit metadata reports the stall, but
// original work and parsed indexes remain; a younger hold still re-arms.
func TestHousekeepingRetainsRowsHeldByWikiPastHoldLimit(t *testing.T) {
	db := setupHousekeepingDB(t)
	queue := &wikiGuardTaskQueue{}
	svc := newHousekeepingSvcForTest(db)
	svc.task = queue
	for id, updatedAt := range map[string]time.Time{
		"k-expired": time.Now().Add(-wikiHoldLimit - time.Hour),
		"k-held":    time.Now().Add(-3 * time.Hour),
	} {
		kbID := "kb-" + id
		require.NoError(t, db.Exec(
			`INSERT INTO knowledges (id, tenant_id, knowledge_base_id, parse_status, updated_at)
			 VALUES (?, 7, ?, ?, ?)`, id, kbID, types.ParseStatusFinalizing, updatedAt,
		).Error)
		insertWikiPendingOp(t, db, kbID, id)
		require.NoError(t, db.Exec("UPDATE task_pending_ops SET tenant_id=7 WHERE dedup_key=?", id).Error)
	}

	svc.runSweep(context.Background())

	status, msg := readKnowledgeStatus(t, db, "k-expired")
	assert.Equal(t, types.ParseStatusFinalizing, status)
	assert.NotEmpty(t, msg)
	assert.Equal(t, int64(1), countWikiOps(t, db, "k-expired"), "original work must remain intact")

	status, _ = readKnowledgeStatus(t, db, "k-held")
	assert.Equal(t, types.ParseStatusFinalizing, status)
	assert.Equal(t, int64(1), countWikiOps(t, db, "k-held"))
	require.Len(t, queue.tasks, 1, "only the live hold gets its trigger re-armed")
	var payload WikiIngestPayload
	require.NoError(t, json.Unmarshal(queue.tasks[0].Payload(), &payload))
	assert.Equal(t, "kb-k-held", payload.KnowledgeBaseID)
}

// A span heartbeat newer than the limit keeps the hold even when the row
// itself has not been written for longer.
func TestSplitExpiredWikiHoldsUsesLatestActivity(t *testing.T) {
	now := time.Now()
	cutoff := now.Add(-wikiHoldLimit)
	held := []types.Knowledge{
		{ID: "row-old-no-beat", UpdatedAt: now.Add(-wikiHoldLimit - time.Hour)},
		{ID: "row-old-recent-beat", UpdatedAt: now.Add(-wikiHoldLimit - time.Hour)},
		{ID: "row-recent", UpdatedAt: now.Add(-time.Hour)},
	}
	heartbeat := map[string]time.Time{"row-old-recent-beat": now.Add(-10 * time.Hour)}

	kept, expired := splitExpiredWikiHolds(held, heartbeat, cutoff)

	require.Len(t, expired, 1)
	assert.Equal(t, "row-old-no-beat", expired[0].ID)
	require.Len(t, kept, 2)
	assert.Equal(t, "row-old-recent-beat", kept[0].ID)
	assert.Equal(t, "row-recent", kept[1].ID)
}

func TestStalledWikiReportCannotOverwriteProgressOrClaimedWork(t *testing.T) {
	for _, mutation := range []string{
		"", "UPDATE knowledges SET updated_at=CURRENT_TIMESTAMP",
		"UPDATE knowledges SET parse_status='completed'",
		"UPDATE knowledges SET error_message='original model failure'",
		"UPDATE task_pending_ops SET claimed_at=CURRENT_TIMESTAMP",
		"UPDATE task_pending_ops SET tenant_id=8",
		"UPDATE task_pending_ops SET scope='archive'",
	} {
		t.Run(mutation, func(t *testing.T) {
			db := setupHousekeepingDB(t)
			stale := time.Now().Add(-wikiHoldLimit - time.Hour)
			require.NoError(t, db.Exec(`INSERT INTO knowledges
 (id,tenant_id,knowledge_base_id,parse_status,updated_at) VALUES ('k',7,'kb','finalizing',?)`, stale).Error)
			insertWikiPendingOp(t, db, "kb", "k")
			require.NoError(t, db.Exec("UPDATE task_pending_ops SET tenant_id=7,fail_count=3").Error)
			if mutation != "" {
				require.NoError(t, db.Exec(mutation).Error)
			}
			// SQLite stores this fixture's JSON as TEXT. Compare the raw row,
			// including its exact payload, without a production JSONB scanner.
			type pendingMetadata struct {
				ID, TenantID                                    uint64
				TaskType, Scope, ScopeID, Op, DedupKey, Payload string
				FailCount                                       int
				EnqueuedAt                                      time.Time
				ClaimedAt                                       *time.Time
			}
			var before, after pendingMetadata
			require.NoError(t, db.Table("task_pending_ops").First(&before).Error)
			svc := newHousekeepingSvcForTest(db)
			svc.recordStalledWikiHolds(context.Background(), []types.Knowledge{{
				ID: "k", TenantID: 7, KnowledgeBaseID: "kb", UpdatedAt: stale,
			}})
			status, message := readKnowledgeStatus(t, db, "k")
			switch mutation {
			case "":
				assert.Contains(t, message, "retained for reviewed recovery")
			case "UPDATE knowledges SET error_message='original model failure'":
				assert.Equal(t, "original model failure", message)
			default:
				assert.Empty(t, message)
			}
			if mutation == "UPDATE knowledges SET parse_status='completed'" {
				assert.Equal(t, types.ParseStatusCompleted, status)
			} else {
				assert.Equal(t, types.ParseStatusFinalizing, status)
			}
			require.NoError(t, db.Table("task_pending_ops").First(&after).Error)
			assert.Equal(t, before, after, "all original task metadata and payload must remain unchanged")
		})
	}
}
