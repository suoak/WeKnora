package datasource

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const syncCatchUpPage = 32

func (s *Scheduler) catchUpScheduled() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.catchUpMissed(ctx, time.Now().UTC()); err != nil {
		logger.Warnf(ctx, "[Scheduler] schedule catch-up deferred: %v", err)
	}
}

// A closed admission gate or a stopped process can miss a cron edge entirely,
// leaving a daily source idle until tomorrow. Examine one metadata page per
// minute and deliver at most one ordinary incremental run per source. The
// normal producer/consumer checks, cooldown and durable admission stay in force.
func (s *Scheduler) catchUpMissed(ctx context.Context, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	repo, ok := s.dsRepo.(interfaces.ScheduledSyncCandidateRepository)
	if !ok {
		return nil
	}
	s.catchUpMu.Lock()
	defer s.catchUpMu.Unlock()
	rows, err := repo.ScheduledSyncCandidates(ctx, s.catchUpAfter, syncCatchUpPage)
	if err != nil {
		return err
	}
	var failures []error
	for _, ds := range rows {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		s.catchUpAfter = ds.ID
		if !ds.AutoSyncEnabled() || LegacyFeishuSyncSuspended(ds.Type) ||
			!LegacyFeishuSourceAllowed(ds.Type, ds.ID) {
			continue
		}
		latest, err := s.syncLogRepo.FindLatest(ctx, ds.ID)
		if err != nil {
			failures = append(failures, fmt.Errorf("source %s schedule history: %w", ds.ID, err))
			continue
		}
		if latest != nil && latest.Status == types.SyncLogStatusRunning {
			continue
		}
		anchor := ds.CreatedAt
		if latest != nil && latest.StartedAt.After(anchor) {
			anchor = latest.StartedAt
		}
		if anchor.IsZero() {
			continue
		}
		schedule, err := syncScheduleParser.Parse(ds.SyncSchedule)
		if err != nil {
			failures = append(failures, fmt.Errorf("source %s schedule invalid", ds.ID))
			continue
		}
		// Spec schedules without CRON_TZ use the cron runner's location, not
		// the UTC location of a persisted SyncLog. Allow the normal tick a
		// minute to admit its run before treating the edge as missed.
		due := schedule.Next(anchor.In(s.cron.Location()))
		if due.IsZero() || due.After(now.Add(-time.Minute)) {
			continue
		}
		logger.Infof(ctx, "[Scheduler] missed schedule detected ds=%s due=%s", ds.ID, due.UTC().Format(time.RFC3339))
		s.triggerSyncContext(ctx, ds.ID, ds.TenantID)
	}
	if len(rows) < syncCatchUpPage {
		s.catchUpAfter = ""
	}
	return errors.Join(failures...)
}
