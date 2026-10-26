// wiki-regenerate is an operator-only preview/apply command. It never certifies
// historical text; the normal worker must regenerate and seal current inputs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/common/wikilock"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func run() error {
	tenant := flag.Uint64("tenant", 0, "exact owning tenant ID")
	kb := flag.String("kb", "", "knowledge base UUID")
	apply := flag.Bool("apply", false, "apply the reviewed snapshot and queue generation")
	snapshot := flag.String("snapshot", "", "snapshot from the preview; required with -apply")
	flag.Parse()
	if *tenant == 0 || *kb == "" {
		return errors.New("-tenant and -kb required")
	}
	dsn := os.Getenv("WIKI_REGEN_POSTGRES_DSN")
	if dsn == "" {
		return errors.New("WIKI_REGEN_POSTGRES_DSN required; credentials must not be passed as arguments")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return errors.New("cannot open regeneration database")
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer func() { _ = pool.Close() }()
	if os.Getenv("REDIS_ADDR") == "" {
		return errors.New("REDIS_ADDR required; offline Lite maintenance is not supported by this command")
	}
	redisDB, err := strconv.Atoi(os.Getenv("REDIS_DB"))
	if err != nil && os.Getenv("REDIS_DB") != "" {
		return errors.New("invalid REDIS_DB")
	}
	opts := asynq.RedisClientOpt{Addr: os.Getenv("REDIS_ADDR"), Password: os.Getenv("REDIS_PASSWORD"), DB: redisDB}
	rc := redis.NewClient(&redis.Options{Addr: opts.Addr, Password: opts.Password, DB: opts.DB})
	defer func() { _ = rc.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var plan *repository.WikiRegenerationPlan
	err = wikilock.With(ctx, rc, *kb, func(locked context.Context) error {
		var e error
		plan, e = repository.PrepareWikiRegeneration(locked, db, *tenant, *kb, *snapshot, *apply)
		return e
	})
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(plan); err != nil {
		return err
	}
	if !*apply {
		return nil
	}
	// Durable ops and slots already committed atomically. Housekeeping re-arms
	// this trigger if Redis is unavailable; rerunning apply cannot duplicate ops.
	client := asynq.NewClient(opts)
	defer func() { _ = client.Close() }()
	payload, err := json.Marshal(map[string]any{"tenant_id": *tenant, "knowledge_base_id": *kb, "language": "zh-CN"})
	if err != nil {
		return err
	}
	_, err = client.EnqueueContext(ctx, asynq.NewTask("wiki:ingest", payload),
		asynq.Queue("wiki"), asynq.MaxRetry(10), asynq.Timeout(time.Hour))
	if err != nil {
		return errors.New("reset committed; trigger enqueue failed; durable operations saved for housekeeping")
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
