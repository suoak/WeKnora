//go:build anydoc

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type v2HandoffSources struct {
	interfaces.DataSourceService
	db         *gorm.DB
	failCreate bool
	resumes    int
}

func (s *v2HandoffSources) CreateDataSource(ctx context.Context, ds *types.DataSource) (*types.DataSource, error) {
	if err := repository.NewDataSourceRepository(s.db).Create(ctx, ds); err != nil {
		return nil, err
	}
	if s.failCreate {
		s.failCreate = false
		return nil, errors.New("injected interruption after source creation")
	}
	return ds, nil
}

func (s *v2HandoffSources) ListDataSources(ctx context.Context, kb string) ([]*types.DataSource, error) {
	rows := []*types.DataSource{}
	err := s.db.WithContext(ctx).Where("knowledge_base_id = ?", kb).Find(&rows).Error
	return rows, err
}

func (s *v2HandoffSources) GetDataSource(ctx context.Context, id string) (*types.DataSource, error) {
	var row types.DataSource
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	return &row, err
}

func (s *v2HandoffSources) ResumeDataSource(ctx context.Context, id string) error {
	s.resumes++
	return s.db.WithContext(ctx).Model(&types.DataSource{}).Where("id = ?", id).
		Update("status", types.DataSourceStatusActive).Error
}

// Exercise the real service and PG handoff with a trusted synthetic receipt.
// This does not claim to generate vectors; the real-parser release gate checks
// those separately. The injected source adapter deliberately owns no publisher.
func TestFeishuV2NativeHandoffRecoversWithoutReset(t *testing.T) {
	f := v2PostgresFixture(t)
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error)
	m, err := f.s.ImportWithIdentity(f.ctx, "kb", "https://test.feishu.cn/file/real", types.FeishuUserOAuth)
	require.NoError(t, err)
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	require.NoError(t, f.db.WithContext(f.ctx).Model(&types.Knowledge{}).
		Where("id = ?", m.LogicalKnowledgeID).Update("parse_status", types.ParseStatusCompleted).Error)
	require.NoError(t, f.db.Model(&types.FeishuPersonalImport{}).
		Where("id = ?", m.ID).Update("status", "indexed").Error)
	require.NoError(t, f.db.Create(&types.TaskPendingOp{
		TenantID: 1, TaskType: "knowledge:index_receipt", Scope: personalIndexReceiptScope,
		ScopeID: m.LogicalKnowledgeID, DedupKey: "index-" + m.AttemptID, Op: "verified",
	}).Error)
	sources := &v2HandoffSources{db: f.db, failCreate: true}
	f.s.dataSources = sources
	_, err = f.s.EnableImportSource(f.ctx, "kb", m.ID, "")
	require.ErrorContains(t, err, "injected interruption")
	ds, err := sources.GetDataSource(f.ctx, m.ID)
	require.NoError(t, err)
	require.Equal(t, types.DataSourceStatusPaused, ds.Status)
	require.False(t, ds.SyncDeletions)
	checkpoint := types.JSON(`{"accepted":"must survive recovery"}`)
	require.NoError(t, f.db.Model(ds).Update("last_sync_cursor", checkpoint).Error)
	ds, err = f.s.EnableImportSource(f.ctx, "kb", m.ID, "")
	require.NoError(t, err)
	require.Equal(t, types.DataSourceStatusActive, ds.Status)
	require.JSONEq(t, string(checkpoint), string(ds.LastSyncCursor))
	require.Equal(t, 1, sources.resumes)
	var current types.FeishuPersonalImport
	require.NoError(t, f.db.Where("id = ?", m.ID).First(&current).Error)
	require.Equal(t, "source_managed", current.Status)
	require.Equal(t, m.LogicalKnowledgeID, current.KnowledgeID)
	var knowledge types.Knowledge
	require.NoError(t, f.db.WithContext(f.ctx).Where("id = ?", current.KnowledgeID).First(&knowledge).Error)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(knowledge.Metadata, &metadata))
	require.Equal(t, m.ID, metadata["datasource_id"])
	require.NoError(t, sources.db.Model(ds).Update("status", types.DataSourceStatusPaused).Error)
	ds, err = f.s.EnableImportSource(f.ctx, "kb", m.ID, "")
	require.NoError(t, err)
	require.Equal(t, types.DataSourceStatusPaused, ds.Status)
	require.Equal(t, 1, sources.resumes, "an existing paused source is never implicitly resumed")
	_, err = f.s.EnableImportSource(f.userContext("B"), "kb", m.ID, "")
	require.Error(t, err)
}
