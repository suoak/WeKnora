package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
)

type derivedTaskVersionKey struct{}

type derivedWriteRepository interface {
	KnowledgeDerivedVersion(context.Context, string) (string, bool, error)
	BeginKnowledgeDerivedWrite(context.Context, string, uint64, string, func(context.Context, *types.DataSource) (context.Context, error)) (context.Context, bool, error)
}

// ConfigureFeishuDerivedWrites uses the same live remote and local source
// authorization as publication. No task-supplied user ID becomes a grant.
func ConfigureFeishuDerivedWrites(knowledge interfaces.KnowledgeService, sources *DataSourceService) {
	if service, ok := knowledge.(*knowledgeService); ok {
		service.derivedSourceBinder = func(ctx context.Context, source *types.DataSource) (context.Context, error) {
			if source.LegacyAccessState != "active" || source.DeletedAt.Valid {
				return ctx, repository.ErrDerivedWriteRejected
			}
			cfg, err := source.ParseConfig()
			if err != nil {
				return ctx, err
			}
			ref, err := types.ParseFeishuSourceReference(cfg)
			if err != nil || ref == nil {
				return ctx, repository.ErrDerivedWriteRejected
			}
			// The persisted source owner's publication consent is revalidated by
			// bindFeishuSource/batchContext, including current membership, app,
			// connection, approval and source permissions. This context is used
			// only to construct the commit guard for this exact source/version.
			bound := context.WithValue(ctx, types.UserIDContextKey, ref.OwnerUserID)
			return sources.bindFeishuSource(bound, source)
		}
	}
}

func knowledgeDerivedVersion(ctx context.Context, repo interfaces.KnowledgeRepository, id string) (string, bool, error) {
	if derived, ok := repo.(derivedWriteRepository); ok {
		return derived.KnowledgeDerivedVersion(ctx, id)
	}
	return "", false, nil
}

func (s *knowledgeService) beginDerivedSummary(ctx context.Context, id string, tenant uint64) (context.Context, bool, error) {
	if derived, ok := s.repo.(derivedWriteRepository); ok {
		expected, _ := ctx.Value(derivedTaskVersionKey{}).(string)
		if expected == "" {
			var err error
			expected, _, err = derived.KnowledgeDerivedVersion(ctx, id)
			if err != nil {
				return ctx, true, err
			}
		}
		return derived.BeginKnowledgeDerivedWrite(ctx, id, tenant, expected, s.derivedSourceBinder)
	}
	return ctx, false, nil
}

func discardDerivedTask(ctx context.Context, id string, err error) bool {
	if !errors.Is(err, repository.ErrDerivedWriteRejected) {
		return false
	}
	logger.Warnf(ctx, "[FeishuDerivedWrite] knowledge_id=%s outcome=discarded reason=version_or_authority_changed", id)
	return true
}

// FeishuTaskResult distinguishes the explicit source fence from unrelated
// PostgreSQL serialization failures, which retain the normal bounded retries.
func FeishuTaskResult(err error) error {
	var state interface{ SQLState() string }
	if errors.Is(err, repository.ErrDerivedWriteRejected) ||
		(errors.As(err, &state) && state.SQLState() == "40001" &&
			strings.Contains(err.Error(), "stale or unmanaged Feishu source write")) {
		return errors.Join(asynq.SkipRetry, err)
	}
	return err
}
