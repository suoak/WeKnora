package feishusynccontrol

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

func TestSystemServiceSQLReceiptRetainsEvidenceAndClosesSplitCommit(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlog.Discard})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	ctx := context.Background()
	f := newNormalFixture(t)
	f.activateNormal(t)
	require.NoError(t, f.m.verifyNormalAuthorization(f.saved.Authorization, false))
	require.NoError(t, StoreNormalServiceRecord(ctx, db, *f.saved, nil))
	original, err := json.Marshal(f.saved)
	require.NoError(t, err)
	state := systemServiceState{
		Version: 1, Epoch: uuid.NewString(), RuntimeSHA: f.c.Config.SourceSHA,
		WorkerID: f.c.Config.WorkerID, WorkerValue: f.c.fingerprint + ":" + f.c.incarnation,
		MembershipRevision: "7", GuardFingerprint: f.c.Guard.Fingerprint(), Phase: "canary",
	}
	raw, err := json.Marshal(state)
	require.NoError(t, err)
	receipt := &systemServiceReceipt{
		Version: 1, Namespace: f.c.prefix, Epoch: state.Epoch,
		RuntimeSHA: state.RuntimeSHA, WorkerValue: state.WorkerValue, Phase: "prepared", StateJSON: string(raw),
		OriginalNormal: string(original), OriginalTrip: "automatic_asynq_consumer_consumer_absent",
	}
	require.NoError(t, prepareSystemReceipt(ctx, db, receipt, nil))
	// Neither a durable preparation alone nor a Redis state without its SQL
	// acknowledgment is execution authority.
	require.ErrorIs(t, checkSystemReceipt(ctx, db, state), ErrNotReady)
	current, err := readSystemReceipt(ctx, db)
	require.NoError(t, err)
	require.Equal(t, *receipt, *current)
	var legacy types.SystemSetting
	require.NoError(t, db.First(&legacy, "key=?", NormalServiceSetting).Error)
	var retired NormalServiceRecord
	require.NoError(t, json.Unmarshal(legacy.Value, &retired))
	require.False(t, retired.Active)
	require.Equal(t, f.saved.Authorization, retired.Authorization, "original real fixture signature is retained exactly")
	var histories []types.SystemSetting
	require.NoError(t, db.Where("key LIKE ?", NormalServiceSetting+".history.%").Find(&histories).Error)
	require.Len(t, histories, 1)
	var historical NormalServiceRecord
	require.NoError(t, json.Unmarshal(histories[0].Value, &historical))
	require.Equal(t, *f.saved, historical)
	require.Error(t, prepareSystemReceipt(ctx, db, receipt, nil), "repeated preparation cannot overwrite a durable epoch")
	require.NoError(t, acknowledgeSystemReceipt(ctx, db, *receipt))
	require.NoError(t, checkSystemReceipt(ctx, db, state))
	require.Error(t, acknowledgeSystemReceipt(ctx, db, *receipt), "lost acknowledgment is reconciled by reading state, not rewriting it")
	changed := state
	changed.Epoch = uuid.NewString()
	require.ErrorIs(t, checkSystemReceipt(ctx, db, changed), ErrNotReady)
	// Emulate the old executable policy reader after the atomic Redis half.
	// This is a protocol compatibility assertion, not a full old-APP rollout.
	require.NoError(t, f.c.Client.Set(ctx, f.c.prefix+"normal_service", receipt.RetiredNormal, 0).Err())
	require.NoError(t, f.c.Client.Set(ctx, f.c.prefix+"recovery_required", "system-service-v1:"+state.Epoch, 0).Err())
	require.NoError(t, f.m.ConfigureNormalService(f.load, f.store, &retired))
	active, err := f.m.NormalServiceActive(ctx)
	require.NoError(t, err)
	require.False(t, active)
	require.Error(t, f.c.checkRecoveryPolicy(ctx), "old incident cannot bypass the system Canary after binary rollback")
}
