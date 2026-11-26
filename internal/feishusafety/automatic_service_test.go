package feishusafety

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type automaticConsumerCASChange struct {
	hash   string
	once   atomic.Bool
	change func() error
}

func (h *automaticConsumerCASChange) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *automaticConsumerCASChange) ProcessPipelineHook(
	next redis.ProcessPipelineHook,
) redis.ProcessPipelineHook {
	return next
}

func (h *automaticConsumerCASChange) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "evalsha" && len(cmd.Args()) > 1 && cmd.Args()[1] == h.hash &&
			h.once.CompareAndSwap(false, true) {
			if err := h.change(); err != nil {
				return err
			}
		}
		return next(ctx, cmd)
	}
}

func TestAutomaticServiceRequiresExactEpochSourceAndRuntime(t *testing.T) {
	for _, fault := range []string{
		"none",
		"healthy-heartbeat-count",
		"healthy-heartbeat-order",
		"metadata-check-race",
		"metadata-budget-race",
		"missing-state",
		"legacy-grant-only",
		"source",
		"phase",
		"epoch",
		"lease",
		"revision",
		"incarnation",
		"validator",
		"consumer-missing",
		"consumer-changed",
		"canary-expired",
	} {
		t.Run(fault, func(t *testing.T) {
			g, client := fixture(t, Config{})
			ctx := context.Background()
			g.RequireAutomaticService()
			g.SetApprovedSourceValidator(func(context.Context, string, string) error { return nil })
			epoch, run := uuid.NewString(), uuid.NewString()
			serverID := uuid.NewString()
			serverKey := "asynq:servers:{worker:123:" + serverID + "}"
			message := automaticConsumerMessage(t, serverID)
			metadata := automaticConsumerWire(t, message)
			metadataDigest := sha1.Sum(metadata)
			state := map[string]any{
				"version": 1, "namespace": g.prefix, "epoch": epoch, "guard_fingerprint": g.Fingerprint(),
				"consumer_server_id": serverID, "consumer_metadata_sha1": hex.EncodeToString(metadataDigest[:]),
				"phase": "canary", "canary_source_id": "source", "canary_run_id": run,
				"canary_started_ms": time.Now().UnixMilli(),
				"worker_id":         "worker:123", "worker_value": "worker:incarnation", "membership_revision": "7",
			}
			raw, err := json.Marshal(state)
			require.NoError(t, err)
			require.NoError(t, client.Set(ctx, g.key("system_service"), raw, 0).Err())
			entry, err := json.Marshal(
				map[string]string{"epoch": epoch, "source_id": "source", "config_sha256": "config"},
			)
			require.NoError(t, err)
			require.NoError(t, client.HSet(ctx, g.key("system_sources"), "source", entry).Err())
			require.NoError(t, client.HSet(ctx, g.key("workers"), "worker:123", "worker:incarnation").Err())
			now, err := client.Time(ctx).Result()
			require.NoError(t, err)
			require.NoError(
				t,
				client.ZAdd(ctx, g.key("leases"), redis.Z{
					Member: "worker:123", Score: float64(now.Add(time.Second).UnixMilli()),
				}).
					Err(),
			)
			require.NoError(t, client.Set(ctx, g.key("membership_revision"), "7", 0).Err())
			require.NoError(t, client.Set(ctx, serverKey, metadata, 10*time.Second).Err())
			require.NoError(
				t,
				client.ZAdd(ctx, "asynq:servers", redis.Z{
					Member: serverKey, Score: float64(now.Add(10 * time.Second).Unix()),
				}).
					Err(),
			)
			work := WithRecoveryContext(
				WithScope(ctx, "source"),
				RecoveryContext{Plan: epoch, Config: "config", Run: run, Generation: 1},
			)
			var mutation *automaticConsumerCASChange
			switch fault {
			case "metadata-check-race", "metadata-budget-race":
				writer := redis.NewClient(client.Options())
				t.Cleanup(func() { require.NoError(t, writer.Close()) })
				hash := checkScript.Hash()
				if fault == "metadata-budget-race" {
					hash = acquireScript.Hash()
				}
				mutation = &automaticConsumerCASChange{hash: hash, change: func() error {
					return writer.Set(ctx, serverKey, "changed-after-semantic-validation", 10*time.Second).
						Err()
				}}
				client.AddHook(mutation)
			case "healthy-heartbeat-count":
				message.Set(
					message.Descriptor().Fields().ByName("active_worker_count"),
					protoreflect.ValueOfInt32(1),
				)
				require.NoError(
					t,
					client.Set(ctx, serverKey, automaticConsumerWire(t, message), 10*time.Second).Err(),
				)
			case "healthy-heartbeat-order":
				require.NoError(
					t,
					client.Set(ctx, serverKey, reversedAutomaticConsumerWire(t, metadata), 10*time.Second).
						Err(),
				)
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
				work = WithRecoveryContext(
					work,
					RecoveryContext{Plan: uuid.NewString(), Config: "config", Run: run, Generation: 1},
				)
			case "lease":
				require.NoError(t, client.ZRem(ctx, g.key("leases"), "worker:123").Err())
			case "revision":
				require.NoError(t, client.Set(ctx, g.key("membership_revision"), "8", 0).Err())
			case "incarnation":
				require.NoError(
					t,
					client.HSet(ctx, g.key("workers"), "worker:123", "worker:replacement").Err(),
				)
			case "validator":
				g.SetApprovedSourceValidator(nil)
			case "consumer-missing":
				require.NoError(t, client.Del(ctx, serverKey).Err())
			case "consumer-changed":
				require.NoError(
					t,
					client.Set(ctx, serverKey, "metadata-changed-after-probe", 10*time.Second).Err(),
				)
			}
			// The budget allocation repeats the same atomic gate as Check.
			checkErr := g.Check(work)
			release, beginErr := g.tryBegin(work, 1)
			if fault == "none" || fault == "healthy-heartbeat-count" || fault == "healthy-heartbeat-order" {
				require.NoError(t, checkErr)
				require.NoError(t, beginErr)
				release()
			} else {
				if fault == "metadata-budget-race" {
					require.NoError(t, checkErr)
				} else {
					require.ErrorIs(t, checkErr, ErrClosed)
				}
				require.ErrorIs(t, beginErr, ErrClosed)
				require.Nil(t, release)
				if mutation != nil {
					require.True(t, mutation.once.Load())
					require.Zero(t, client.HLen(ctx, g.key("inflight")).Val(),
						"no slot is spent on an unverified changed heartbeat")
					require.Zero(t, client.Exists(ctx, g.key("trip")).Val(),
						"an atomic observation race denies without inventing a new incident")
				}
			}
		})
	}
}
