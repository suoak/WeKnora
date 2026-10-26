package container

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime/debug"
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
	configured := false
	defer func() {
		if configured {
			return
		}
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if rdb != nil {
			_ = rdb.Set(stopCtx, "asynq:{sync}:paused", "invalid_feishu_safety_configuration", 0).Err()
		}
		feishusynccontrol.CancelLocal()
		if _, err := repository.FenceLegacySyncExecutions(stopCtx, db); err != nil {
			logger.Errorf(ctx, "[FeishuSafety] invalid configuration stop fencing incomplete")
		}
	}()
	mode := os.Getenv("FEISHU_SYNC_SAFETY_MODE")
	if mode != "" && mode != "automatic" && mode != "strict" {
		logger.Errorf(ctx, "[FeishuSafety] unknown safety mode; outbound closed")
		return
	}
	automatic := mode != "strict"
	policy := feishusafety.DefaultConfig()
	if !automatic {
		policy = feishusafety.Config{}
	}
	policyJSON := os.Getenv("FEISHU_SYNC_SAFETY_POLICY_JSON")
	if policyJSON == "" && automatic {
		encoded, _ := json.Marshal(policy)
		policyJSON = string(encoded)
	}
	decoder := json.NewDecoder(strings.NewReader(policyJSON))
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
	var alert func(feishusynccontrol.Event) error
	alertURL := os.Getenv("FEISHU_SYNC_SAFETY_ALERT_URL")
	if alertURL != "" || !automatic {
		alert, err = feishusynccontrol.NewAlertSink(alertURL, nil)
	}
	if err != nil {
		logger.Errorf(ctx, "[FeishuSafety] acknowledged alert receiver missing; outbound closed")
		return
	}
	allocation := types.ResolveWorkerPoolConcurrency(func(key, env string, fallback int) int {
		return int(settings.GetInt(ctx, key, env, int64(fallback)))
	})
	cfg := feishusynccontrol.Config{
		Automatic:       automatic,
		WorkerID:        os.Getenv("FEISHU_SYNC_WORKER_ID"),
		ExpectedWorkers: strings.Split(os.Getenv("FEISHU_SYNC_EXPECTED_WORKERS"), ","),
		SourceSHA:       os.Getenv("FEISHU_SYNC_SOURCE_SHA"),
		Scope:           os.Getenv("FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS"), Maintenance: allocation.Maintenance,
	}
	if automatic {
		host, hostErr := os.Hostname()
		if hostErr != nil {
			return
		}
		// Asynq registry uses this same physical process identity.
		if cfg.WorkerID == "" {
			cfg.WorkerID = fmt.Sprintf("%s:%d", host, os.Getpid())
		}
		cfg.ExpectedWorkers = nil
		if cfg.Scope == "" {
			cfg.Scope = "all"
		}
		if cfg.SourceSHA == "" {
			cfg.SourceSHA = "build-unversioned"
			if info, ok := debug.ReadBuildInfo(); ok {
				for _, setting := range info.Settings {
					if setting.Key == "vcs.revision" {
						cfg.SourceSHA = setting.Value
					}
				}
			}
		}
	}
	controller, err := feishusynccontrol.New(rdb, guard, cfg, func(stopCtx context.Context) error {
		_, fenceErr := repository.FenceLegacySyncExecutions(stopCtx, db)
		return fenceErr
	}, func(event feishusynccontrol.Event) error {
		if event.Kind == "feishu_safety_heartbeat" {
			logger.Debugf(
				context.Background(),
				"[FeishuSafety] event=%s reason=%s worker=%s",
				event.Kind,
				event.Reason,
				event.WorkerID,
			)
		} else {
			logger.Errorf(context.Background(), "[FeishuSafety] event=%s reason=%s worker=%s fenced=%t queue_paused=%t",
				event.Kind, event.Reason, event.WorkerID, event.Fenced, event.QueuePaused)
		}
		if alert != nil {
			return alert(event)
		}
		// The default sink is durable Redis audit plus structured application logs.
		// It is not a claim of external paging/online health monitoring.
		auditCtx, cancel := context.WithTimeout(event.Context(), time.Second)
		defer cancel()
		return rdb.XAdd(auditCtx, &redis.XAddArgs{
			Stream: strings.TrimSuffix(guard.PermitKey(), "permit") + "events", MaxLen: 1000, Approx: true,
			Values: map[string]any{
				"kind":         event.Kind,
				"reason":       event.Reason,
				"worker_id":    event.WorkerID,
				"fenced":       event.Fenced,
				"queue_paused": event.QueuePaused,
			},
		}).
			Err()
	})
	if err != nil {
		logger.Errorf(ctx, "[FeishuSafety] worker inventory or effective concurrency invalid; outbound closed")
		return
	}
	if automatic {
		controller.Consumers = feishusynccontrol.SyncConsumers(rdb)
	}
	feishusafety.InstallDefault(guard)
	configured = true
	runCtx, cancel := context.WithCancel(ctx)
	go monitorFeishuStorage(runCtx, rdb, strings.TrimSuffix(guard.PermitKey(), "permit"))
	done := make(chan struct{})
	go func() { defer close(done); controller.Run(runCtx) }()
	cleaner.RegisterWithName("FeishuSyncSafety", func() error {
		cancel()
		select {
		case <-done:
		case <-time.After(7 * time.Second):
		}
		feishusafety.InstallDefault(nil)
		return nil
	})
}
