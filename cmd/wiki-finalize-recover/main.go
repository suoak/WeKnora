// wiki-finalize-recover previews and re-arms only an archived contention head.
// It never rewrites SQL operations or runs an archived failure in place.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func run() error {
	tenant := flag.Uint64("tenant", 0, "exact owning tenant")
	kb := flag.String("kb", "", "knowledge base UUID")
	expected := flag.String("snapshot", "", "reviewed preview snapshot")
	apply := flag.Bool("apply", false, "re-arm the existing contention successor once")
	flag.Parse()
	if *tenant == 0 || *kb == "" || *apply && *expected == "" {
		return errors.New("tenant, kb and apply snapshot are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := gorm.Open(postgres.Open(os.Getenv("WIKI_REGEN_POSTGRES_DSN")), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return errors.New("cannot open recovery database")
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer func() { _ = pool.Close() }()
	var rows []types.TaskPendingOp
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("knowledge_bases AS kb").Joins("JOIN tenants t ON t.id=kb.tenant_id").
			Where("kb.id=? AND kb.tenant_id=? AND kb.deleted_at IS NULL AND t.deleted_at IS NULL", *kb, *tenant).
			Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return errors.New("live tenant and knowledge base required")
		}
		return tx.Where("scope=? AND scope_id=?", types.TaskScopeKnowledgeBase, *kb).
			Order("id").Find(&rows).Error
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errors.New("no pending finalize operations")
	}
	for _, row := range rows {
		if row.TenantID != *tenant || row.TaskType != types.TypeWikiFinalize || row.ClaimedAt != nil {
			return errors.New("only unclaimed finalize operations of the owning tenant are supported")
		}
	}
	redisDB, err := strconv.Atoi(os.Getenv("REDIS_DB"))
	if err != nil && os.Getenv("REDIS_DB") != "" {
		return errors.New("invalid Redis database")
	}
	rc := redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR"), Password: os.Getenv("REDIS_PASSWORD"), DB: redisDB})
	defer func() { _ = rc.Close() }()
	inspector := asynq.NewInspectorFromRedisClient(rc)
	canonical := "wiki-finalize-" + *kb
	base, err := inspector.GetTaskInfo(types.QueueWiki, canonical)
	if err != nil || base.State != asynq.TaskStateArchived {
		return errors.New("canonical trigger must remain archived")
	}
	headID := fmt.Sprintf("%s-pending-%d", canonical, rows[0].ID)
	head, err := inspector.GetTaskInfo(types.QueueWiki, headID)
	if err != nil || head.State != asynq.TaskStateArchived || head.LastErr != service.ErrWikiIngestConcurrent.Error() {
		return errors.New("durable head must be archived with the exact contention error")
	}
	for _, info := range []*asynq.TaskInfo{base, head} {
		var p service.WikiIngestPayload
		if info.Type != types.TypeWikiFinalize || json.Unmarshal(info.Payload, &p) != nil ||
			p.TenantID != *tenant || p.KnowledgeBaseID != *kb {
			return errors.New("archived trigger routing mismatch")
		}
	}
	nextID := headID + "-contention"
	if _, err := inspector.GetTaskInfo(types.QueueWiki, nextID); !errors.Is(err, asynq.ErrTaskNotFound) {
		return errors.New("successor already exists or lookup is unconfirmed")
	}
	data, err := json.Marshal([]any{rows, base, head})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	snapshot := hex.EncodeToString(digest[:])
	if *apply && snapshot != *expected {
		return errors.New("preview snapshot changed")
	}
	if *apply {
		err := service.RearmWikiFinalize(ctx, asynq.NewClientFromRedisClient(rc),
			repository.NewTaskPendingOpsRepository(db), rc,
			service.WikiIngestPayload{TenantID: *tenant, KnowledgeBaseID: *kb, Language: "zh-CN"})
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"tenant_id": *tenant, "knowledge_base_id": *kb, "pending_operations": len(rows),
		"head_id": rows[0].ID, "archived_task": headID, "last_error": head.LastErr,
		"successor_id": nextID, "snapshot": snapshot, "applied": *apply,
	})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
