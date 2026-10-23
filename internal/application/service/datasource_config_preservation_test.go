package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type configPreservationConnector struct {
	datasource.Connector
	validations int
}

func (*configPreservationConnector) Type() string { return types.ConnectorTypeFeishu }

func (c *configPreservationConnector) Validate(context.Context, *types.DataSourceConfig) error {
	c.validations++
	return nil
}

func TestScheduleOnlyEncryptedConfigPreservesAccessP0(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	t.Setenv("FEISHU_LEGACY_SYNC_SUSPENDED", "false")
	for _, accessState := range []string{"active", "unmanaged", "hold", "revoked"} {
		t.Run(accessState, func(t *testing.T) {
			f := newSQLiteDataSourceDeleteFixture(t)
			cfg := &types.DataSourceConfig{
				Type: f.ds.Type, ResourceIDs: []string{"selected"}, Settings: map[string]interface{}{"mode": "blocks"},
				Credentials: map[string]interface{}{"app_id": "synthetic", "app_secret": "synthetic-secret"},
			}
			stored, err := cfg.ToJSON()
			require.NoError(t, err)
			renonced, err := cfg.ToJSON()
			require.NoError(t, err)
			require.NotEqual(t, string(stored), string(renonced), "same plaintext uses random AES-GCM nonces")
			require.NoError(t, f.db.Model(f.ds).Updates(map[string]interface{}{
				"config": stored, "legacy_access_state": accessState,
			}).Error)
			guarded := types.EnableLegacyReadGuards(f.db)
			repo := repository.NewDataSourceRepository(guarded)
			connector := &configPreservationConnector{}
			registry := datasource.NewConnectorRegistry()
			require.NoError(t, registry.Register(connector))
			svc := &DataSourceService{dsRepo: repo, scheduler: f.scheduler, connectorRegistry: registry}
			for _, incoming := range []types.JSON{renonced, nil} {
				updated := &types.DataSource{
					ID: f.ds.ID, TenantID: f.ds.TenantID, KnowledgeBaseID: f.ds.KnowledgeBaseID,
					SyncSchedule: "0 30 * * * *", Config: incoming,
				}
				_, err = svc.UpdateDataSource(context.Background(), updated)
				require.NoError(t, err)
				saved, err := repo.FindByID(context.Background(), f.ds.ID)
				require.NoError(t, err)
				require.Equal(t, accessState, saved.LegacyAccessState, "schedule writes preserve access state")
				require.JSONEq(t, string(stored), string(saved.Config), "stored ciphertext must remain unchanged")
			}
			require.Zero(t, connector.validations, "unchanged config must not issue a live permission probe")
			_, err = svc.UpdateDataSource(context.Background(), &types.DataSource{
				ID: f.ds.ID, Type: types.ConnectorTypeFeishuDrive, SyncSchedule: "0 30 * * * *",
			})
			require.ErrorIs(t, err, datasource.ErrDataSourceInvalid,
				"omitted configuration cannot bypass validation for a new connector type")
			if accessState == "revoked" {
				return
			}
			cfg.ResourceIDs = []string{"different-selection"}
			changed, err := cfg.ToJSON()
			require.NoError(t, err)
			_, err = svc.UpdateDataSource(context.Background(), &types.DataSource{
				ID: f.ds.ID, Type: f.ds.Type, Config: changed, SyncSchedule: "0 30 * * * *",
			})
			require.NoError(t, err)
			saved, err := repo.FindByID(context.Background(), f.ds.ID)
			require.NoError(t, err)
			require.Equal(t, "hold", saved.LegacyAccessState, "actual resource changes still require verification")
			require.Equal(t, 1, connector.validations)
		})
	}
}
