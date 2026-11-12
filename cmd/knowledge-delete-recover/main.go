// knowledge-delete-recover previews a dedicated source-handoff deletion fence,
// then lets the original, archived deletion task perform its normal cleanup.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var errPreviewRollback = errors.New("preview rollback")

// The persisted task is an exact identity, not authorization. Recheck the live
// actor and parent, and use the same repository fence as the ordinary worker.
func fence(
	ctx context.Context, db *gorm.DB, p types.KnowledgeListDeletePayload, expected string, apply bool,
) (string, error) {
	if p.TenantID == 0 || p.KnowledgeBaseID == "" || len(p.KnowledgeIDs) != 1 ||
		p.KnowledgeIDs[0] == "" || p.Initiator.UserID == "" || p.Initiator.APIKeyID != 0 ||
		types.IsSyntheticUserID(p.Initiator.UserID) {
		return "", errors.New("one exact web-user deletion task is required")
	}
	var snapshot string
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout='15s'; SET LOCAL lock_timeout='5s'").Error; err != nil {
			return err
		}
		var member struct {
			Role      types.TenantRole
			CreatorID string
		}
		err := tx.Table("tenant_members m").Select("m.role,k.creator_id").
			Joins("JOIN users u ON u.id=m.user_id JOIN tenants t ON t.id=m.tenant_id "+
				"JOIN knowledge_bases k ON k.tenant_id=t.id").
			Where(`m.user_id=? AND m.tenant_id=? AND k.id=? AND m.status='active' AND m.deleted_at IS NULL
AND u.is_active AND u.deleted_at IS NULL AND t.status='active' AND t.deleted_at IS NULL AND k.deleted_at IS NULL`,
				p.Initiator.UserID, p.TenantID, p.KnowledgeBaseID).Take(&member).Error
		if err != nil || !member.Role.IsValid() ||
			(!member.Role.HasPermission(types.TenantRoleAdmin) && member.CreatorID != p.Initiator.UserID) {
			return errors.New("deletion actor is no longer authorized")
		}
		ctx = p.Initiator.Apply(ctx)
		ctx = types.WithCaller(ctx, types.Caller{TenantID: p.TenantID, UserID: p.Initiator.UserID, Role: member.Role})
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: p.Initiator.UserID})
		ctx = types.WithExecutionTenant(ctx, p.TenantID)
		ctx = types.WithKnowledgeDeletionMetadata(ctx, p.KnowledgeIDs[0])
		tx = types.EnablePrivateKBGuards(tx).WithContext(ctx)
		repo := repository.NewKnowledgeRepository(tx)
		rows, err := repo.GetKnowledgeDeletionMetadata(ctx, p.TenantID, p.KnowledgeIDs)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].KnowledgeBaseID != p.KnowledgeBaseID || rows[0].Channel != "feishu_personal" {
			return errors.New("deletion document binding changed")
		}
		if err := access.RejectMovingKnowledge(rows[0]); err != nil {
			return err
		}
		var identity []struct {
			ImportID         string
			Generation       int64
			AttemptID        string
			SourceUpdated    time.Time
			KnowledgeUpdated time.Time
		}
		if err := tx.Raw(`SELECT i.id AS import_id,r.generation,r.current_attempt_id AS attempt_id,
d.updated_at AS source_updated,k.updated_at AS knowledge_updated FROM knowledges k
JOIN feishu_personal_imports i ON i.knowledge_id=k.id AND i.kb_id=k.knowledge_base_id AND i.status='source_managed'
JOIN feishu_personal_resources r ON r.import_id=i.id
JOIN data_sources d ON d.id=i.id AND d.deleted_at IS NULL
WHERE k.id=? AND k.tenant_id=? AND k.deleted_at IS NULL FOR UPDATE OF d`, p.KnowledgeIDs[0], p.TenantID).
			Scan(&identity).Error; err != nil {
			return err
		}
		if len(identity) != 1 {
			return errors.New("one live source handoff is required")
		}
		blob, err := json.Marshal([]any{p, identity})
		if err != nil {
			return err
		}
		digest := sha256.Sum256(blob)
		snapshot = hex.EncodeToString(digest[:])
		if apply && (expected == "" || expected != snapshot) {
			return errors.New("preview snapshot changed")
		}
		if err := repo.MarkKnowledgeDeleting(ctx, rows[0]); err != nil {
			return err
		}
		if !apply {
			return errPreviewRollback
		}
		return nil
	})
	if errors.Is(err, errPreviewRollback) {
		err = nil
	}
	return snapshot, err
}

func run() error {
	taskID := flag.String("task", "", "original archived deletion task ID")
	tenant := flag.Uint64("tenant", 0, "exact tenant")
	kb := flag.String("kb", "", "exact knowledge base")
	kid := flag.String("knowledge", "", "exact document")
	expected := flag.String("snapshot", "", "reviewed preview snapshot")
	apply := flag.Bool("apply", false, "fence the dedicated source and retry the original task once")
	flag.Parse()
	if *taskID == "" || *tenant == 0 || *kb == "" || *kid == "" || (*apply && *expected == "") {
		return errors.New("exact task, tenant, kb, knowledge and apply snapshot are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := gorm.Open(postgres.Open(os.Getenv("KNOWLEDGE_RECOVERY_POSTGRES_DSN")),
		&gorm.Config{Logger: logger.Discard})
	if err != nil {
		return errors.New("cannot open recovery database")
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer func() { _ = pool.Close() }()
	redisDB, err := strconv.Atoi(os.Getenv("REDIS_DB"))
	if err != nil && os.Getenv("REDIS_DB") != "" {
		return errors.New("invalid Redis DB")
	}
	rc := redis.NewClient(&redis.Options{
		Addr: os.Getenv("REDIS_ADDR"), Password: os.Getenv("REDIS_PASSWORD"), DB: redisDB,
	})
	defer func() { _ = rc.Close() }()
	inspector := asynq.NewInspectorFromRedisClient(rc)
	info, err := inspector.GetTaskInfo(types.QueueMaintenance, *taskID)
	if err != nil {
		return err
	}
	if info.Type != types.TypeKnowledgeListDelete || info.State != asynq.TaskStateArchived ||
		info.LastErr != "personal deletion attempt unverified" {
		return errors.New("task must be archived with the exact handoff deletion error")
	}
	var payload types.KnowledgeListDeletePayload
	if json.Unmarshal(info.Payload, &payload) != nil || payload.TenantID != *tenant || payload.KnowledgeBaseID != *kb ||
		len(payload.KnowledgeIDs) != 1 || payload.KnowledgeIDs[0] != *kid {
		return errors.New("archived deletion routing mismatch")
	}
	snapshot, err := fence(ctx, db, payload, *expected, *apply)
	if err != nil {
		return err
	}
	if *apply {
		if err := inspector.RunTask(types.QueueMaintenance, *taskID); err != nil {
			return fmt.Errorf("fence committed; original task retry failed: %w", err)
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"task_id": *taskID, "tenant_id": *tenant, "knowledge_base_id": *kb,
		"knowledge_id": *kid, "snapshot": snapshot, "applied": *apply,
		"original_last_error": info.LastErr, "original_retry_count": info.Retried,
	})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
