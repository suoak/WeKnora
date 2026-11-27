package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUploadTagCreateNameValidation(t *testing.T) {
	f := newFAQWriteFixture(t)
	for _, name := range []string{"", "  ", "bad\x00name", "bad\nname", strings.Repeat("标", 129)} {
		_, err := f.tags.CreateTag(f.ctx, "kb", name, "", 0)
		var appErr *werrors.AppError
		require.ErrorAs(t, err, &appErr)
		require.Equal(t, 400, appErr.HTTPCode)
	}
	tag, err := f.tags.CreateTag(f.ctx, "kb", "  first tag  ", "", 0)
	require.NoError(t, err)
	require.Equal(t, "first tag", tag.Name)
	require.Equal(t, uint64(7), tag.TenantID)
	_, err = f.tags.CreateTag(f.ctx, "kb", "first tag", "", 0)
	var appErr *werrors.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 409, appErr.HTTPCode)
}

// Force the interleaving in which the preflight misses and another request
// inserts the same name before our insert reaches the unique index.
type tagCreateRaceRepository struct {
	interfaces.KnowledgeTagRepository
	err error
}

func (r tagCreateRaceRepository) GetByName(
	ctx context.Context, tenant uint64, kb, name string,
) (*types.KnowledgeTag, error) {
	return r.KnowledgeTagRepository.GetByName(ctx, tenant, kb, name)
}

func (r tagCreateRaceRepository) Create(ctx context.Context, tag *types.KnowledgeTag) error {
	if r.err != nil {
		return r.err
	}
	if err := r.KnowledgeTagRepository.Create(ctx, tag); err != nil {
		return err
	}
	loser := *tag
	loser.ID = "losing-request"
	loser.SeqID = 999
	return r.KnowledgeTagRepository.Create(ctx, &loser)
}

func TestUploadTagCreateConcurrentConflict(t *testing.T) {
	f := newFAQWriteFixture(t)
	require.NoError(t, f.db.Exec(
		"CREATE UNIQUE INDEX idx_tag_create_test ON knowledge_tags(tenant_id, knowledge_base_id, name)",
	).Error)
	f.tags.repo = tagCreateRaceRepository{KnowledgeTagRepository: f.tags.repo}
	_, err := f.tags.CreateTag(f.ctx, "kb", "raced", "", 0)
	var appErr *werrors.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 409, appErr.HTTPCode)
	var count int64
	require.NoError(t, f.db.Model(&types.KnowledgeTag{}).Where("name = ?", "raced").Count(&count).Error)
	require.EqualValues(t, 1, count)
	existing, err := f.tags.repo.GetByName(f.ctx, 7, "kb", "raced")
	require.NoError(t, err)
	require.NotEmpty(t, existing.ID)
	_, err = f.tags.repo.GetByName(f.ctx, 8, "kb", "raced")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestUploadTagCreateDatabaseFailureRemainsFailure(t *testing.T) {
	f := newFAQWriteFixture(t)
	dbErr := errors.New("database unavailable")
	f.tags.repo = tagCreateRaceRepository{KnowledgeTagRepository: f.tags.repo, err: dbErr}
	_, err := f.tags.CreateTag(f.ctx, "kb", "failed", "", 0)
	require.ErrorIs(t, err, dbErr)
}
