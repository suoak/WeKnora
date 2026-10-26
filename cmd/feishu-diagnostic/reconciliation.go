package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// This isolated operator path does not clear a latch or resume a queue. A fresh
// live permit and the deliberately still-paused sync queue are prerequisites.
// Actual source permissions, generation and publication are checked by Worker.
func enqueueApprovedReconciliation(
	ctx context.Context, db *gorm.DB, policy *service.FeishuSyncPublicationPolicy,
) error {
	if policy.AuthorizationRef != reconciliationAuthorization || policy.State != "approved" ||
		!policy.ReconciliationExpiresAt.After(time.Now().Add(time.Minute)) || policy.ReconciliationLogID == "" ||
		mask(policy.SourceID) != "1dcf458346ba" {
		return errors.New("approved_held_source_reconciliation_required")
	}
	var run types.SyncLog
	if err := db.Where("id = ? AND data_source_id = ? AND tenant_id = ? AND status = ?",
		policy.ReconciliationLogID, policy.SourceID, policy.TenantID, types.SyncLogStatusRunning).
		First(&run).Error; err != nil || run.ExecutionClaimed || run.ExecutionGeneration != 0 {
		return errors.New("reconciliation_run_already_claimed_or_terminal")
	}
	database, err := strconv.Atoi(os.Getenv("REDIS_DB"))
	if err != nil {
		database = 0
	}
	if os.Getenv("REDIS_ADDR") == "" || database != 0 {
		return errors.New("isolated_queue_configuration_required")
	}
	options := &redis.Options{
		Addr: os.Getenv("REDIS_ADDR"), Username: os.Getenv("REDIS_USERNAME"),
		Password: os.Getenv("REDIS_PASSWORD"), DB: database, TLSConfig: common.RedisTLSConfig(),
	}
	probe := redis.NewClient(options)
	defer probe.Close()
	gate := "feishu:safety:{legacy-feishu}:"
	trip, err := probe.Exists(ctx, gate+"trip").Result()
	if err != nil || trip != 0 {
		return errors.New("reconciliation_stop_latch_not_recovered")
	}
	permit, err := probe.PTTL(ctx, gate+"permit").Result()
	if err != nil || permit < 100*time.Millisecond {
		return errors.New("reconciliation_live_permit_required")
	}
	paused, err := probe.Get(ctx, "asynq:{sync}:paused").Result()
	if err != nil || paused != "feishu_safety_stop" {
		return errors.New("reconciliation_controlled_queue_pause_required")
	}
	payload, err := json.Marshal(types.DataSourceSyncPayload{
		DataSourceID: policy.SourceID, TenantID: policy.TenantID, SyncLogID: policy.ReconciliationLogID,
		ForceFull: true, Trigger: "approved_reconciliation",
	})
	if err != nil {
		return err
	}
	queue := asynq.NewClient(asynq.RedisClientOpt{
		Addr: options.Addr, Username: options.Username, Password: options.Password, DB: options.DB,
		TLSConfig: options.TLSConfig,
	})
	defer queue.Close()
	_, err = queue.EnqueueContext(ctx, asynq.NewTask(types.TypeDataSourceSync, payload),
		asynq.TaskID("feishu-approved-reconciliation:"+policy.ReconciliationLogID),
		asynq.Queue(types.QueueSync), asynq.MaxRetry(3), asynq.Timeout(15*time.Minute))
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil // Retrying the operator enqueue never creates a second run.
	}
	return err
}
