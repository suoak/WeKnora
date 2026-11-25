package feishusynccontrol

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestObservationPostgresRevocationSurvivesReloadAndRedisFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "reload", true: "redis_write_failed"}[failure], func(t *testing.T) {
			db := syncdb.Open(t, "postgres")
			require.NoError(t, db.AutoMigrate(&types.SystemSetting{}))
			f := newNormalFixture(t)
			ctx := context.Background()
			store := func(ctx context.Context, record NormalServiceRecord, previous *NormalServiceRecord) error {
				if err := StoreNormalServiceRecord(ctx, db, record, previous); err != nil {
					return err
				}
				f.saved = &record
				if !record.Active && failure {
					// Fault after the durable SQL commit, before Redis publication.
					return f.r.Close()
				}
				return nil
			}
			require.NoError(t, f.m.ConfigureNormalService(f.load, store, nil))
			f.activateNormal(t)
			err := f.m.DeactivateNormalService(ctx)
			if failure {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			var setting types.SystemSetting
			require.NoError(t, db.First(&setting, "key=?", NormalServiceSetting).Error)
			var persisted NormalServiceRecord
			require.NoError(t, json.Unmarshal(setting.Value, &persisted))
			require.False(t, persisted.Active)
			var histories int64
			require.NoError(t, db.Model(&types.SystemSetting{}).
				Where("key LIKE ?", NormalServiceSetting+".history.%").Count(&histories).Error)
			require.EqualValues(t, 1, histories)
			if failure {
				client := redis.NewClient(f.r.Options())
				t.Cleanup(func() { _ = client.Close() })
				f.c.Client = client
			}
			public := ed25519.NewKeyFromSeed(f.key).Public().(ed25519.PublicKey)
			reloaded, err := NewRecoveryManager(f.c, public, f.m.facts)
			require.NoError(t, err)
			require.NoError(t, reloaded.ConfigureNormalService(f.load, store, &persisted))
			active, err := reloaded.NormalServiceActive(ctx)
			require.False(t, active, "reload cannot recreate installation-wide authority")
			if failure {
				require.Error(t, err, "SQL/Redis disagreement stays closed; no automatic repair")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
