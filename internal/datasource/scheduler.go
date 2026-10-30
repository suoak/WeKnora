package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/runtime"
	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/robfig/cron/v3"
)

var syncScheduleParser = cron.NewParser(
	cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// ValidateSyncSchedule accepts the same syntax as the scheduler. An empty
// schedule disables automatic sync and remains valid for manual-only sources.
func ValidateSyncSchedule(schedule string) error {
	if schedule == "" {
		return nil
	}
	if _, err := syncScheduleParser.Parse(schedule); err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", schedule, err)
	}
	return nil
}

// Scheduler manages cron-based periodic sync for data sources.
//
// robfig/cron fires at absolute wall-clock times (e.g. "0 0 * * * *" always fires
// at the top of every hour regardless of when the process started). So multiple
// instances will fire at the same moment. Dedup is handled by two layers:
//
//  1. HasRunningSync — if a previous sync is still running, skip (prevent overlap).
//  2. asynq.TaskID  — deterministic ID per (dataSourceID, minute). Redis ensures
//     only one task with a given ID is enqueued. Losers get ErrTaskIDConflict.
type Scheduler struct {
	cron         *cron.Cron
	dsRepo       interfaces.DataSourceRepository
	syncLogRepo  interfaces.SyncLogRepository
	taskEnqueuer interfaces.TaskEnqueuer

	mu      sync.Mutex
	entries map[string]cron.EntryID // dataSourceID → cron entry ID
}

// NewScheduler creates a new Scheduler.
func NewScheduler(
	dsRepo interfaces.DataSourceRepository,
	syncLogRepo interfaces.SyncLogRepository,
	taskEnqueuer interfaces.TaskEnqueuer,
) *Scheduler {
	return &Scheduler{
		cron: cron.New(cron.WithParser(syncScheduleParser), cron.WithChain(
			cron.Recover(cron.DefaultLogger),
		)),
		dsRepo:       dsRepo,
		syncLogRepo:  syncLogRepo,
		taskEnqueuer: taskEnqueuer,
		entries:      make(map[string]cron.EntryID),
	}
}

// Start loads all active data sources from the database and registers their
// cron schedules. Then starts the cron runner in the background.
func (s *Scheduler) Start(ctx context.Context) error {
	dataSources, err := s.dsRepo.FindActive(ctx)
	if err != nil {
		return fmt.Errorf("load active data sources: %w", err)
	}

	for _, ds := range dataSources {
		if !ds.AutoSyncEnabled() {
			continue
		}
		if err := s.addEntry(ds); err != nil {
			logger.Warnf(ctx, "[Scheduler] failed to register cron for ds=%s schedule=%q: %v",
				ds.ID, ds.SyncSchedule, err)
		}
	}

	s.cron.Start()
	logger.Infof(ctx, "[Scheduler] started with %d cron entries", len(s.entries))
	return nil
}

// Stop gracefully stops the cron runner and waits for running jobs to finish.
func (s *Scheduler) Stop() {
	s.StopWithin(0)
}

// StopWithin is Stop with a bound. timeout <= 0 waits until in-flight jobs
// finish. A positive timeout lets shutdown continue so one long sync cannot
// hold every later cleanup hook, including child-process reaping.
func (s *Scheduler) StopWithin(timeout time.Duration) {
	if s == nil || s.cron == nil {
		return
	}
	if runtime.WaitFor(s.cron.Stop().Done(), timeout) {
		return
	}
	logger.Warnf(context.Background(),
		"[Scheduler] in-flight sync still running after %s; continuing shutdown", timeout)
}

// AddOrUpdate registers (or re-registers) a cron entry for the given data source.
func (s *Scheduler) AddOrUpdate(ds *types.DataSource) error {
	// Reject an invalid replacement before removing the working entry. Inactive
	// sources must still be removable even if their stored schedule is invalid.
	if ds.AutoSyncEnabled() {
		if err := ValidateSyncSchedule(ds.SyncSchedule); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, ok := s.entries[ds.ID]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, ds.ID)
	}

	if !ds.AutoSyncEnabled() || LegacyFeishuSyncSuspended(ds.Type) || !LegacyFeishuSourceAllowed(ds.Type, ds.ID) {
		return nil
	}

	return s.addEntryLocked(ds)
}

// Remove removes the cron entry for a data source.
func (s *Scheduler) Remove(dataSourceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, ok := s.entries[dataSourceID]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, dataSourceID)
	}
}

func (s *Scheduler) addEntry(ds *types.DataSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addEntryLocked(ds)
}

func (s *Scheduler) addEntryLocked(ds *types.DataSource) error {
	if LegacyFeishuSyncSuspended(ds.Type) || !LegacyFeishuSourceAllowed(ds.Type, ds.ID) {
		return nil
	}
	dsID := ds.ID
	tenantID := ds.TenantID

	entryID, err := s.cron.AddFunc(ds.SyncSchedule, func() {
		s.triggerSync(dsID, tenantID)
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", ds.SyncSchedule, err)
	}

	s.entries[dsID] = entryID
	return nil
}

// triggerSync is called by the cron runner on each tick.
//
// Layer 1 — DB: atomically admit a running sync log, shared with ManualSync.
// This prevents duplicate admission while that log remains non-terminal;
// it is not an execution lease and does not fence retries of failed tasks.
//
// Layer 2 — Redis: deterministic asynq.TaskID = "dssync:<dsID>:<minute>".
// Since robfig/cron fires at absolute wall-clock times, all instances trigger
// at the same minute. The first Enqueue wins; others get ErrTaskIDConflict.
func (s *Scheduler) triggerSync(dataSourceID string, tenantID uint64) {
	ctx := context.Background()

	ds, err := s.dsRepo.FindByID(ctx, dataSourceID)
	if err != nil || !ds.AutoSyncEnabled() {
		logger.Infof(ctx, "[Scheduler] skipping sync for ds=%s (not active or not found)", dataSourceID)
		return
	}
	if LegacyFeishuSyncSuspended(ds.Type) || !LegacyFeishuSourceAllowed(ds.Type, ds.ID) {
		return
	}
	queueName := types.QueueSync
	if feishusafety.Scoped(feishusafety.LegacyContext(ctx, ds.Type, ds.ID)) {
		fingerprint, fpErr := feishusafety.SourceConfigFingerprint(
			ds.ID,
			ds.KnowledgeBaseID,
			ds.Type,
			ds.LegacyAccessState,
			ds.TenantID,
			ds.Config,
		)
		if fpErr != nil {
			return
		}
		var approved bool
		ctx, approved, err = feishusynccontrol.BindApprovedSource(ctx, ds.ID, fingerprint)
		if err != nil {
			return
		}
		if approved {
			queueName = types.QueueApprovedSync
		}
	}
	if err := feishusafety.CheckLegacy(ctx, ds.Type, ds.ID); err != nil {
		logger.Warnf(ctx, "[Scheduler] sync safety gate closed ds=%s", ds.ID)
		return
	}
	// Keep the user's cron. Repeated failures suppress only ticks inside a
	// bounded cooldown; successful/manual runs reset the failure streak.
	history, err := s.syncLogRepo.FindByDataSource(ctx, dataSourceID, 6, 0)
	if err != nil {
		logger.Warnf(ctx, "[Scheduler] cannot read retry history ds=%s: %v", dataSourceID, err)
		return
	}
	if !syncRetryDue(history, time.Now().UTC()) {
		logger.Infof(ctx, "[Scheduler] skipping sync ds=%s phase=cooldown category=recent_failures", dataSourceID)
		return
	}

	syncLog := &types.SyncLog{
		DataSourceID: dataSourceID,
		TenantID:     tenantID,
		Status:       types.SyncLogStatusRunning,
		StartedAt:    time.Now().UTC(),
	}
	created, err := s.syncLogRepo.CreateIfNoRunning(ctx, syncLog)
	if err != nil {
		logger.Errorf(ctx, "[Scheduler] failed to create sync log for ds=%s: %v", dataSourceID, err)
		return
	}
	if !created {
		logger.Infof(ctx, "[Scheduler] skipping sync for ds=%s (sync already queued or running)", dataSourceID)
		return
	}

	payload := &types.DataSourceSyncPayload{
		DataSourceID: dataSourceID,
		TenantID:     tenantID,
		SyncLogID:    syncLog.ID,
		ForceFull:    false,
		Trigger:      "schedule",
	}
	langfuse.InjectTracing(ctx, payload)
	payloadJSON, _ := json.Marshal(payload)
	task := asynq.NewTask(types.TypeDataSourceSync, payloadJSON)

	// Layer 2: deterministic TaskID — all instances in the same minute produce the same ID
	taskID := fmt.Sprintf("dssync:%s:%s", dataSourceID, time.Now().UTC().Truncate(time.Minute).Format("200601021504"))

	_, err = s.taskEnqueuer.Enqueue(task,
		asynq.Queue(queueName),
		asynq.MaxRetry(5),
		asynq.Timeout(2*time.Hour),
		asynq.TaskID(taskID),
	)
	if err != nil {
		if err == asynq.ErrTaskIDConflict {
			logger.Infof(ctx, "[Scheduler] sync already enqueued by another instance for ds=%s", dataSourceID)
			syncLog.Status = types.SyncLogStatusCanceled
			now := time.Now().UTC()
			syncLog.FinishedAt = &now
			syncLog.ErrorMessage = "deduplicated: another instance enqueued first"
			_ = s.syncLogRepo.Update(ctx, syncLog)
			return
		}
		logger.Errorf(ctx, "[Scheduler] failed to enqueue sync task for ds=%s: %v", dataSourceID, err)
		syncLog.Status = types.SyncLogStatusFailed
		now := time.Now().UTC()
		syncLog.FinishedAt = &now
		syncLog.ErrorMessage = fmt.Sprintf("enqueue failed: %v", err)
		_ = s.syncLogRepo.Update(ctx, syncLog)
		return
	}

	logger.Infof(ctx, "[Scheduler] sync task enqueued for ds=%s syncLog=%s", dataSourceID, syncLog.ID)
}

func syncRetryDue(history []*types.SyncLog, now time.Time) bool {
	streak := 0
	var failedAt time.Time
	for _, log := range history {
		if log.Status != types.SyncLogStatusFailed {
			break
		}
		if streak == 0 {
			failedAt = log.UpdatedAt
			if log.FinishedAt != nil {
				failedAt = *log.FinishedAt
			}
		}
		streak++
	}
	if streak == 0 || failedAt.IsZero() {
		return true
	}
	delay := min(5*time.Minute*time.Duration(1<<min(streak-1, 4)), time.Hour)
	return !now.Before(failedAt.Add(delay))
}

// EntryCount returns the number of active cron entries (for testing/monitoring).
func (s *Scheduler) EntryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}
