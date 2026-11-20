package datasource

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/require"
)

type catchUpSourceRepo struct {
	*fakeDataSourceRepo
	afterRead func()
}

func (r *catchUpSourceRepo) ScheduledSyncCandidates(
	ctx context.Context, after string, limit int,
) ([]*types.DataSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	var rows []*types.DataSource
	for _, source := range r.dataSources {
		if source.ID > after {
			copySource := *source
			rows = append(rows, &copySource)
		}
	}
	r.mu.Unlock()
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	if len(rows) > limit {
		rows = rows[:limit]
	}
	if r.afterRead != nil {
		r.afterRead()
	}
	return rows, nil
}

type catchUpLogRepo struct {
	*fakeSyncLogRepo
	errorID string
}

func (r *catchUpLogRepo) FindLatest(_ context.Context, id string) (*types.SyncLog, error) {
	if id == r.errorID {
		return nil, errors.New("history temporarily unavailable")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var latest *types.SyncLog
	for _, row := range r.logs {
		if row.DataSourceID == id && (latest == nil || row.StartedAt.After(latest.StartedAt)) {
			latest = row
		}
	}
	return latest, nil
}

func TestSchedulerCatchUpMissedEdgesRetainsUserControlsAndTimezone(t *testing.T) {
	now := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	for _, scenario := range []string{
		"missed", "already_attempted", "running", "paused", "manual", "held", "revoked",
		"new", "unknown_creation", "invalid", "grace", "platform_stopped",
	} {
		t.Run(scenario, func(t *testing.T) {
			repo := &catchUpSourceRepo{fakeDataSourceRepo: newFakeDataSourceRepo()}
			logs := &catchUpLogRepo{fakeSyncLogRepo: newFakeSyncLogRepo()}
			queue := &fakeTaskEnqueuer{}
			source := &types.DataSource{
				ID: "source", TenantID: 1, Type: "gitlab", Status: "active", SyncSchedule: "0 0 2 * * *",
				CreatedAt: now.Add(-72 * time.Hour),
			}
			var history *types.SyncLog
			want := int64(0)
			switch scenario {
			case "missed":
				want = 1
			case "already_attempted":
				// 03:00 Shanghai: the next local 02:00 tick is tomorrow.
				history = &types.SyncLog{Status: "success", StartedAt: now.Add(-13 * time.Hour)}
			case "running":
				history = &types.SyncLog{Status: "running", StartedAt: now.Add(-48 * time.Hour)}
			case "paused":
				source.Status = "paused"
			case "manual":
				source.SyncSchedule = ""
			case "held":
				source.LegacyAccessState = "hold"
			case "revoked":
				source.LegacyAccessState = "revoked"
			case "new":
				source.CreatedAt = now.Add(-time.Hour)
			case "unknown_creation":
				source.CreatedAt = time.Time{}
			case "invalid":
				source.SyncSchedule = "invalid"
			case "grace":
				source.SyncSchedule = "@every 6h"
				source.CreatedAt = now.Add(-6*time.Hour - 30*time.Second)
			case "platform_stopped":
				source.Type = "feishu"
				t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "true")
			}
			require.NoError(t, repo.Create(context.Background(), source))
			if history != nil {
				history.DataSourceID = source.ID
				require.NoError(t, logs.Create(context.Background(), history))
			}
			s := NewScheduler(repo, logs, queue)
			s.cron = cron.New(cron.WithParser(syncScheduleParser), cron.WithLocation(time.FixedZone("CST", 8*3600)))
			err := s.catchUpMissed(context.Background(), now)
			if scenario == "invalid" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, want, queue.count.Load())
			if want > 0 {
				require.NoError(t, s.catchUpMissed(context.Background(), now))
				require.Equal(t, want, queue.count.Load(), "a queued run is durable admission, not another missed edge")
				require.Equal(t, types.QueueGeneralSync, queue.lastQueue.Load())
			}
		})
	}
}

func TestSchedulerCatchUpPagesFairlyAndRetainsAtomicAdmission(t *testing.T) {
	repo := &catchUpSourceRepo{fakeDataSourceRepo: newFakeDataSourceRepo()}
	logs := &catchUpLogRepo{fakeSyncLogRepo: newFakeSyncLogRepo()}
	queue := &fakeTaskEnqueuer{}
	now := time.Now().UTC()
	for i := 0; i < 64; i++ {
		require.NoError(t, repo.Create(context.Background(), &types.DataSource{
			ID: fmt.Sprintf("%03d", i), TenantID: 1, Type: "gitlab", Status: "active", SyncSchedule: "@every 6h",
			CreatedAt: now.Add(-24 * time.Hour),
		}))
	}
	s := NewScheduler(repo, logs, queue)
	require.NoError(t, s.catchUpMissed(context.Background(), now))
	require.EqualValues(t, 32, queue.count.Load())
	require.NoError(t, s.catchUpMissed(context.Background(), now))
	require.EqualValues(t, 64, queue.count.Load())
	require.NoError(t, s.catchUpMissed(context.Background(), now))
	require.NoError(t, s.catchUpMissed(context.Background(), now))
	require.EqualValues(t, 64, queue.count.Load())
	// Multiple instances may discover the same overdue sources. They still use
	// the same atomic running-log admission as cron and manual synchronization.
	other := NewScheduler(repo, logs, queue)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = other.catchUpMissed(context.Background(), now) }()
	}
	wg.Wait()
	require.EqualValues(t, 64, queue.count.Load())
}

func TestSchedulerCatchUpRechecksPauseAndContinuesAfterHistoryFailure(t *testing.T) {
	repo := &catchUpSourceRepo{fakeDataSourceRepo: newFakeDataSourceRepo()}
	logs := &catchUpLogRepo{fakeSyncLogRepo: newFakeSyncLogRepo(), errorID: "error"}
	queue := &fakeTaskEnqueuer{}
	now := time.Now().UTC()
	for _, id := range []string{"error", "healthy", "paused_after_scan"} {
		require.NoError(t, repo.Create(context.Background(), &types.DataSource{
			ID: id, TenantID: 1, Type: "gitlab", Status: "active", SyncSchedule: "@every 6h",
			CreatedAt: now.Add(-24 * time.Hour),
		}))
	}
	repo.afterRead = func() {
		paused := &types.DataSource{
			ID: "paused_after_scan", TenantID: 1, Type: "gitlab", Status: "paused", SyncSchedule: "@every 6h",
		}
		require.NoError(t, repo.Update(context.Background(), paused))
	}
	err := NewScheduler(repo, logs, queue).catchUpMissed(context.Background(), now)
	require.Error(t, err)
	require.EqualValues(
		t,
		1,
		queue.count.Load(),
		"a history failure and a paused sibling cannot block a healthy source",
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, NewScheduler(repo, logs, queue).catchUpMissed(ctx, now), context.Canceled)
	require.EqualValues(t, 1, queue.count.Load())
}
