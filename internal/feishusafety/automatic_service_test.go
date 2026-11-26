package feishusafety

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAutomaticServiceRequiresExactEpochSourceAndRuntime(t *testing.T) {
	for _, fault := range []string{"none", "missing-state", "legacy-grant-only", "source", "phase", "epoch", "lease", "revision", "incarnation", "validator", "consumer-missing", "consumer-changed", "canary-expired"} {
		t.Run(fault, func(t *testing.T) {
			g, client := fixture(t, Config{})
			ctx := context.Background()
			g.RequireAutomaticService()
			g.SetApprovedSourceValidator(func(context.Context, string, string) error { return nil })
			epoch, run := uuid.NewString(), uuid.NewString()
			serverID := uuid.NewString()
			serverKey := "asynq:servers:{test:1:" + serverID + "}"
			metadataDigest := sha1.Sum([]byte("fixture-consumer-protobuf"))
			state := map[string]any{
				"version": 1, "namespace": g.prefix, "epoch": epoch, "guard_fingerprint": g.Fingerprint(),
				"consumer_server_id": serverID, "consumer_metadata_sha1": hex.EncodeToString(metadataDigest[:]),
				"phase": "canary", "canary_source_id": "source", "canary_run_id": run,
				"canary_started_ms": time.Now().UnixMilli(),
				"worker_id":         "test:1", "worker_value": "worker:incarnation", "membership_revision": "7",
			}
			raw, err := json.Marshal(state)
			require.NoError(t, err)
			require.NoError(t, client.Set(ctx, g.key("system_service"), raw, 0).Err())
			entry, err := json.Marshal(map[string]string{"epoch": epoch, "source_id": "source", "config_sha256": "config"})
			require.NoError(t, err)
			require.NoError(t, client.HSet(ctx, g.key("system_sources"), "source", entry).Err())
			require.NoError(t, client.HSet(ctx, g.key("workers"), "test:1", "worker:incarnation").Err())
			now, err := client.Time(ctx).Result()
			require.NoError(t, err)
			require.NoError(t, client.ZAdd(ctx, g.key("leases"), redis.Z{Member: "test:1", Score: float64(now.Add(time.Second).UnixMilli())}).Err())
			require.NoError(t, client.Set(ctx, g.key("membership_revision"), "7", 0).Err())
			require.NoError(t, client.Set(ctx, serverKey, "fixture-consumer-protobuf", 10*time.Second).Err())
			require.NoError(t, client.ZAdd(ctx, "asynq:servers", redis.Z{Member: serverKey, Score: float64(now.Add(10 * time.Second).Unix())}).Err())
			work := WithRecoveryContext(WithScope(ctx, "source"), RecoveryContext{Plan: epoch, Config: "config", Run: run, Generation: 1})
			switch fault {
			case "missing-state":
				require.NoError(t, client.Del(ctx, g.key("system_service")).Err())
			case "legacy-grant-only":
				require.NoError(t, client.Del(ctx, g.key("system_service")).Err())
				require.NoError(t, client.Set(ctx, g.key("normal_service"), `{"active":true}`, 0).Err())
			case "source":
				work = WithScope(work, "another-source")
			case "phase":
				state["phase"] = "unknown"
				raw, err = json.Marshal(state)
				require.NoError(t, err)
				require.NoError(t, client.Set(ctx, g.key("system_service"), raw, 0).Err())
			case "canary-expired":
				state["canary_started_ms"] = time.Now().Add(-6 * time.Minute).UnixMilli()
				raw, err = json.Marshal(state)
				require.NoError(t, err)
				require.NoError(t, client.Set(ctx, g.key("system_service"), raw, 0).Err())
			case "epoch":
				work = WithRecoveryContext(work, RecoveryContext{Plan: uuid.NewString(), Config: "config", Run: run, Generation: 1})
			case "lease":
				require.NoError(t, client.ZRem(ctx, g.key("leases"), "test:1").Err())
			case "revision":
				require.NoError(t, client.Set(ctx, g.key("membership_revision"), "8", 0).Err())
			case "incarnation":
				require.NoError(t, client.HSet(ctx, g.key("workers"), "test:1", "worker:replacement").Err())
			case "validator":
				g.SetApprovedSourceValidator(nil)
			case "consumer-missing":
				require.NoError(t, client.Del(ctx, serverKey).Err())
			case "consumer-changed":
				require.NoError(t, client.Set(ctx, serverKey, "metadata-changed-after-probe", 10*time.Second).Err())
			}
			// The budget allocation repeats the same atomic gate as Check.
			checkErr := g.Check(work)
			release, beginErr := g.tryBegin(work, 1)
			if fault == "none" {
				require.NoError(t, checkErr)
				require.NoError(t, beginErr)
				release()
			} else {
				require.ErrorIs(t, checkErr, ErrClosed)
				require.ErrorIs(t, beginErr, ErrClosed)
				require.Nil(t, release)
			}
		})
	}
}
