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
	deadline := flag.Duration("timeout", 2*time.Minute, "bounded operator deadline between 1m and 10m")
	resume := flag.Bool("resume-pending-ingest", false,
		"replace only unclaimed Wiki-only finalizing operations from verified inputs")
	retireEmpty := flag.Bool("retire-empty-retractions", false,
		"preview/apply archival of deleted-source retractions with no queued or current Wiki targets")
	isolatePages := flag.Bool("isolate-unverified-pages", false,
		"preserve valid pages and original operations; isolate only pages failing current proof")
	restoreParsed := flag.Bool("restore-parsed-stalls", false,
		"preview/apply verified native parsed documents misclassified by housekeeping; preserve old work")
	flag.Parse()
	if *retireEmpty && *resume || *isolatePages && (*resume || *retireEmpty) ||
		*restoreParsed && (*resume || *retireEmpty || *isolatePages) {
		return errors.New("choose one recovery mode")
	}
	if *tenant == 0 || *kb == "" {
		return errors.New("-tenant and -kb required")
	}
	if *deadline < time.Minute || *deadline > 10*time.Minute {
		return errors.New("operator deadline must be between 1m and 10m")
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
	opts := asynq.RedisClientOpt{
		Addr:     os.Getenv("REDIS_ADDR"),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       redisDB,
	}
	rc := redis.NewClient(&redis.Options{Addr: opts.Addr, Password: opts.Password, DB: opts.DB})
	defer func() { _ = rc.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), *deadline)
	defer cancel()
	if *restoreParsed {
		var plan *repository.WikiParsedStallPlan
		err := wikilock.With(ctx, rc, *kb, func(locked context.Context) error {
			var e error
			plan, e = repository.PrepareWikiParsedStallRecovery(locked, db, *tenant, *kb, *snapshot, *apply)
			return e
		})
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(plan)
	}
	if *retireEmpty {
		var plan *repository.WikiEmptyRetractionPlan
		err := wikilock.With(ctx, rc, *kb, func(locked context.Context) error {
			var e error
			plan, e = repository.PrepareWikiEmptyRetractions(locked, db, *tenant, *kb, *snapshot, *apply)
			return e
		})
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(plan)
	}
	if *isolatePages {
		var plan *repository.WikiPageRecoveryPlan
		err := wikilock.With(ctx, rc, *kb, func(locked context.Context) error {
			var e error
			plan, e = repository.PrepareWikiPageRecovery(locked, db, *tenant, *kb, *snapshot, *apply)
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
		return enqueueGeneration(ctx, opts, *tenant, *kb)
	}
	var plan *repository.WikiRegenerationPlan
	err = wikilock.With(ctx, rc, *kb, func(locked context.Context) error {
		var e error
		plan, e = repository.PrepareWikiRegeneration(locked, db, *tenant, *kb, *snapshot, *apply, *resume)
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
	return enqueueGeneration(ctx, opts, *tenant, *kb)
}

func enqueueGeneration(ctx context.Context, opts asynq.RedisClientOpt, tenant uint64, kb string) error {
	// Durable ops and slots already committed atomically. Housekeeping re-arms
	// this trigger if Redis is unavailable; rerunning apply cannot duplicate ops.
	client := asynq.NewClient(opts)
	defer func() { _ = client.Close() }()
	payload, err := json.Marshal(
		map[string]any{"tenant_id": tenant, "knowledge_base_id": kb, "language": "zh-CN"},
	)
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
