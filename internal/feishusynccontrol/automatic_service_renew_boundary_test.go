package feishusynccontrol

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
	"gorm.io/gorm"
)

func renewableSQLService(t *testing.T) (automaticAuthorityFixture, *automaticService, *redis.Client) {
	t.Helper()
	f := newAutomaticAuthorityFixture(t)
	health, r, opt := nativeAutomaticService(t)
	startNativeAutomaticConsumer(t, opt)
	c := health.c
	m, err := NewAutomaticServiceManager(c, f.db,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes))
	require.NoError(t, err)
	previous := Recovery()
	InstallRecovery(m)
	t.Cleanup(func() { InstallRecovery(previous) })
	nativeAutomaticStable(t, m.system)
	require.NoError(t, c.Step(context.Background()))
	return f, m.system, r
}

// Reorder complete protobuf fields from an ACTUAL running SDK consumer. This
// is an explicit isolated fixture write, not a claim of an autonomous heartbeat
// or remote Feishu access. Every field/value, including unknown fields, survives.
func reorderActualConsumerMetadata(t *testing.T, r *redis.Client, key string) {
	t.Helper()
	ctx := context.Background()
	raw, err := r.Get(ctx, key).Bytes()
	require.NoError(t, err)
	ttl, err := r.PTTL(ctx, key).Result()
	require.NoError(t, err)
	require.Positive(t, ttl)
	var fields [][]byte
	for rest := raw; len(rest) > 0; {
		_, _, n := protowire.ConsumeField(rest)
		require.Positive(t, n)
		fields = append(fields, append([]byte(nil), rest[:n]...))
		rest = rest[n:]
	}
	var changed []byte
	for i := len(fields) - 1; i >= 0; i-- {
		changed = append(changed, fields[i]...)
	}
	require.False(t, bytes.Equal(raw, changed))
	desc, err := protoregistry.GlobalFiles.FindDescriptorByName("asynq.ServerInfo")
	require.NoError(t, err)
	messageDesc, ok := desc.(protoreflect.MessageDescriptor)
	require.True(t, ok)
	a, b := dynamicpb.NewMessage(messageDesc), dynamicpb.NewMessage(messageDesc)
	require.NoError(t, proto.Unmarshal(raw, a))
	require.NoError(t, proto.Unmarshal(changed, b))
	require.True(t, proto.Equal(a, b), "only encoding order changes, never subscription or identity")
	require.NoError(t, r.Set(ctx, key, changed, ttl).Err())
}

func TestAutomaticServiceNativeSQLRefreshRenewProofAfterSQLBoundary(t *testing.T) {
	for _, mode := range []string{"sdk_equivalent_metadata", "legitimate_source_registration"} {
		t.Run(mode, func(t *testing.T) {
			f, s, r := renewableSQLService(t)
			ctx := context.Background()
			before, _, err := s.current(ctx)
			require.NoError(t, err)
			var sourceWork context.Context
			var runID string
			if mode == "legitimate_source_registration" {
				logs := repository.NewSyncLogRepository(f.db)
				log := &types.SyncLog{
					ID: uuid.NewString(), DataSourceID: f.source.ID,
					TenantID: f.tenant.ID, Status: types.SyncLogStatusRunning, StartedAt: time.Now().UTC(),
				}
				created, createErr := logs.CreateIfNoRunning(ctx, log)
				require.NoError(t, createErr)
				require.True(t, created)
				work, handled, prepareErr := PrepareAutomaticSourceExecution(ctx, f.source.ID, log.ID, f.tenant.ID)
				require.NoError(t, prepareErr)
				require.True(t, handled)
				owner, claimed, claimErr := logs.(interfaces.SyncExecutionRepository).
					ClaimExecution(work, f.source.ID, log.ID, f.tenant.ID)
				require.NoError(t, claimErr)
				require.True(t, claimed)
				sourceWork = feishusafety.WithRecoveryExecution(
					types.WithSyncExecution(work, owner), f.config, log.ID, owner.Generation)
				runID = log.ID
			}
			var queries atomic.Int32
			var injected atomic.Bool
			callback := "test:renew_sql_boundary_" + uuid.NewString()
			require.NoError(t, f.db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				if !strings.Contains(tx.Statement.SQL.String(), "system_settings") || queries.Add(1) != 4 {
					return
				}
				injected.Store(true)
				if mode == "sdk_equivalent_metadata" {
					key := "asynq:servers:{" + before.WorkerID + ":" + before.ConsumerServerID + "}"
					reorderActualConsumerMetadata(t, r, key)
				} else {
					_, bindErr := s.bindSource(sourceWork, f.source.ID, f.config)
					require.NoError(t, bindErr, "use real SQL claim, authority checks and registration Lua")
				}
			}))
			t.Cleanup(func() { require.NoError(t, f.db.Callback().Query().Remove(callback)) })
			// Old code rejects the obsolete snapshot exactly here. There is no
			// retry/eventually around the positive Step, timeout extension or fake clock.
			err = s.c.Step(ctx)
			require.True(t, injected.Load(), "must reach the actual SQL-to-CAS boundary")
			require.NoError(t, err, "healthy concurrent updates must be sampled after SQL")
			require.False(t, s.c.Guard.DependencySuspended())
			require.Equal(t, s.c.Guard.Fingerprint(), r.Get(ctx, s.c.Guard.PermitKey()).Val())
			after, _, err := s.current(ctx)
			require.NoError(t, err)
			require.Equal(t, before.Epoch, after.Epoch)
			require.Equal(t, before.WorkerValue, after.WorkerValue)
			if mode == "legitimate_source_registration" {
				require.Equal(t, f.source.ID, after.CanarySourceID, "renew must not overwrite the new binding")
				require.Equal(t, runID, after.CanaryRunID)
				require.NoError(t, s.c.Guard.Check(feishusafety.WithScope(sourceWork, f.source.ID)))
			}
		})
	}
}
