package container

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Invalid rollout settings close only marked Feishu work. Online model calls
// retain their old path. Startup health does not attest a deployable sync gate.
func startFeishuSyncControl(db *gorm.DB, rdb *redis.Client,
	settings interfaces.SystemSettingService, cleaner interfaces.ResourceCleaner,
) {
	if !feishusafety.Enabled() {
		return
	}
	ctx := context.Background()
	feishusafety.InstallDefault(nil)
	var policy feishusafety.Config
	decoder := json.NewDecoder(strings.NewReader(os.Getenv("FEISHU_SYNC_SAFETY_POLICY_JSON")))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil || rdb == nil {
		logger.Errorf(ctx, "[FeishuSafety] configuration invalid; all marked Feishu outbound closed")
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		logger.Errorf(ctx, "[FeishuSafety] trailing configuration; outbound closed")
		return
	}
	guard, err := feishusafety.New(rdb, policy)
	if err != nil {
		logger.Errorf(ctx, "[FeishuSafety] verified budget contract missing; outbound closed")
		return
	}
	allocation := types.ResolveWorkerPoolConcurrency(func(key, env string, fallback int) int {
		return int(settings.GetInt(ctx, key, env, int64(fallback)))
	})
	cfg := feishusynccontrol.Config{
		WorkerID:        os.Getenv("FEISHU_SYNC_WORKER_ID"),
		ExpectedWorkers: strings.Split(os.Getenv("FEISHU_SYNC_EXPECTED_WORKERS"), ","),
		SourceSHA:       os.Getenv("FEISHU_SYNC_SOURCE_SHA"),
		Scope:           os.Getenv("FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS"), Maintenance: allocation.Maintenance,
	}
	controller, err := feishusynccontrol.New(rdb, guard, cfg, func(stopCtx context.Context) error {
		_, fenceErr := repository.FenceLegacySyncExecutions(stopCtx, db)
		return fenceErr
	}, func(event feishusynccontrol.Event) error {
		// Deployment must connect structured errors to an alert receiver; this
		// log is durable local evidence, not a claim that paging was delivered.
		logger.Errorf(context.Background(), "[FeishuSafety] event=%s reason=%s worker=%s fenced=%t queue_paused=%t",
			event.Kind, event.Reason, event.WorkerID, event.Fenced, event.QueuePaused)
		return nil
	})
	if err != nil {
		logger.Errorf(ctx, "[FeishuSafety] worker inventory or effective concurrency invalid; outbound closed")
		return
	}
	feishusafety.InstallDefault(guard)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); controller.Run(runCtx) }()
	cleaner.RegisterWithName("FeishuSyncSafety", func() error {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		feishusafety.InstallDefault(nil)
		return nil
	})
}
