package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource"
	feishu "github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Synthetic external connector; service, execution repository, parser, model
// HTTP client and PostgreSQL index writer remain the actual implementations.
// Real Wiki/Drive HTTP protocol coverage is a separate connector/APP gate.
type legacySyncFixture struct {
	datasource.Connector
	kind  string
	item  *types.FetchedItem
	err   error
	calls int
}

func (f *legacySyncFixture) Type() string { return f.kind }
func (f *legacySyncFixture) FetchStream(ctx context.Context, _ *types.DataSourceConfig, _ *types.SyncCursor, h datasource.StreamHandler) (*types.SyncCursor, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if f.item != nil {
		if err := h.Emit(ctx, *f.item); err != nil {
			return nil, err
		}
	}
	cursor := &types.SyncCursor{ConnectorCursor: map[string]interface{}{"verified_run": f.calls}}
	if err := h.Checkpoint(ctx, cursor); err != nil {
		return nil, err
	}
	return cursor, nil
}
func (f *legacySyncFixture) FetchFullStream(ctx context.Context, c *types.DataSourceConfig, p *types.SyncCursor, h datasource.StreamHandler) (*types.SyncCursor, error) {
	return f.FetchStream(ctx, c, p, h)
}

type legacyNoAutoTag struct{ interfaces.KnowledgeTagService }

func (*legacyNoAutoTag) FindOrCreateTagByName(context.Context, string, string) (*types.KnowledgeTag, error) {
	return nil, errors.New("synthetic tag unavailable")
}

func exerciseLegacyProcessSync(t *testing.T, db *gorm.DB, ks *knowledgeService, ds *types.DataSource, item *types.FetchedItem, kid string) {
	t.Helper()
	fixture := &legacySyncFixture{kind: ds.Type, item: item}
	registry := datasource.NewConnectorRegistry()
	require.NoError(t, registry.Register(fixture))
	svc := &DataSourceService{dsRepo: repository.NewDataSourceRepository(db), syncLogRepo: repository.NewSyncLogRepository(db), knowledgeService: ks, kbService: ks.kbService, tenantRepo: repository.NewTenantRepository(db), tagService: &legacyNoAutoTag{}, connectorRegistry: registry}
	run := func(full bool) (*types.SyncLog, *asynq.Task, error) {
		log := &types.SyncLog{ID: uuid.NewString(), DataSourceID: ds.ID, TenantID: ds.TenantID, Status: types.SyncLogStatusRunning}
		require.NoError(t, db.Create(log).Error)
		payload, _ := json.Marshal(types.DataSourceSyncPayload{DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: ds.TenantID, ForceFull: full})
		task := asynq.NewTask("datasource:sync", payload)
		err := svc.ProcessSync(context.Background(), task)
		require.NoError(t, db.First(log, "id=?", log.ID).Error)
		return log, task, err
	}
	log, task, err := run(false)
	require.NoError(t, err)
	require.Equal(t, types.SyncLogStatusSuccess, log.Status)
	var saved types.DataSource
	require.NoError(t, db.First(&saved, "id=?", ds.ID).Error)
	confirmed := string(saved.LastSyncCursor)
	require.Contains(t, confirmed, "verified_run")
	calls := fixture.calls
	require.NoError(t, svc.ProcessSync(context.Background(), task))
	require.Equal(t, calls, fixture.calls, "duplicate terminal delivery must not call connector")
	fixture.err = &feishu.APIError{Category: feishu.ErrorCategoryRateLimit}
	log, _, err = run(false)
	require.Error(t, err)
	require.Equal(t, types.SyncLogStatusFailed, log.Status)
	require.NoError(t, db.First(&saved, "id=?", ds.ID).Error)
	require.Equal(t, confirmed, string(saved.LastSyncCursor))
	require.NoError(t, types.CheckLegacySourceRead(db, kid, ds.KnowledgeBaseID))
	fixture.err = &feishu.APIError{Category: feishu.ErrorCategoryPermission}
	_, _, err = run(false)
	require.Error(t, err)
	require.NoError(t, db.First(&saved, "id=?", ds.ID).Error)
	require.Equal(t, confirmed, string(saved.LastSyncCursor))
	require.Equal(t, "hold", saved.LegacyAccessState)
	require.ErrorIs(t, types.CheckLegacySourceRead(db, kid, ds.KnowledgeBaseID), types.ErrLegacySourceRestricted)
	fixture.err = nil
	log, _, err = run(true)
	require.NoError(t, err)
	require.Equal(t, types.SyncLogStatusSuccess, log.Status)
	require.NoError(t, db.First(&saved, "id=?", ds.ID).Error)
	require.Equal(t, "active", saved.LegacyAccessState)
	require.NoError(t, types.CheckLegacySourceRead(db, kid, ds.KnowledgeBaseID))
}
