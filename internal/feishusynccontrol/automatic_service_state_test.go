package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSystemServiceStableIdentityWindow(t *testing.T) {
	proof := systemHealthProof{
		WorkerID: "test:1", WorkerValue: "fingerprint:incarnation", MembershipRevision: "7",
		Metadata: map[string]string{"asynq:servers:{test}": "digest"}, ConsumerServerIDs: []string{"server"},
	}
	start := time.Now()
	var stable stableServiceHealth
	for i := 0; i < 15; i++ {
		require.False(t, stable.observe(start.Add(time.Duration(i)*200*time.Millisecond), proof, nil))
	}
	require.True(t, stable.observe(start.Add(3*time.Second), proof, nil))
	require.True(t, stable.observe(start.Add(3*time.Second+time.Millisecond), proof, nil), "an additional healthy read does not invalidate an already proven window")
	// A heartbeat byte change is normal; a registry/consumer identity change is not.
	proof.Metadata["asynq:servers:{test}"] = "new-heartbeat-digest"
	require.True(t, stable.observe(start.Add(3200*time.Millisecond), proof, nil))
	proof.ConsumerServerIDs = []string{"replacement-server"}
	require.False(t, stable.observe(start.Add(3400*time.Millisecond), proof, nil))
	require.False(t, stable.observe(start.Add(3600*time.Millisecond), proof, errors.New("read failure")))
	require.Zero(t, stable.samples)
	for i := 0; i < 100; i++ {
		require.False(t, stable.observe(start.Add(4*time.Second), proof, nil), "same-instant probes cannot create stability")
	}
	require.False(t, stable.observe(start.Add(6*time.Second), proof, nil), "long observation gap restarts the window")
}

func TestSystemServiceMigrationAtomicEvidenceAndFaults(t *testing.T) {
	for _, fault := range []string{
		"none", "membership", "metadata", "lease", "trip", "old-work", "audit", "incarnation",
		"operator-stop", "unknown-stop", "consumer-lost-stop",
		"original-audit-missing", "original-audit-mismatch", "audit-tail-changed", "observation-changed", "required-changed",
	} {
		t.Run(fault, func(t *testing.T) {
			ctx := context.Background()
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			prefix := "feishu:safety:{disposable-system-test}:"
			keys := []string{
				prefix + "workers", prefix + "leases", prefix + "membership_revision", prefix + "admitted",
				"asynq:servers", prefix + "trip", prefix + "system_service", prefix + "normal_service",
				prefix + "recovery", prefix + "observation_required", prefix + "inflight",
				"asynq:{approved_sync}:active", "asynq:{sync}:active", prefix + "system_journal",
				prefix + "audit", prefix + "control_audit", prefix + "recovery_required", prefix + "permit",
				prefix + "system_commit:test-epoch",
			}
			serverID := uuid.NewString()
			metadataKey := "asynq:servers:{test:1:" + serverID + "}"
			workerValue := "0123456789012345678901234567890123456789012345678901234567890123:" + uuid.NewString()
			now, err := client.Time(ctx).Result()
			require.NoError(t, err)
			require.NoError(t, client.HSet(ctx, keys[0], "test:1", workerValue).Err())
			require.NoError(t, client.ZAdd(ctx, keys[1], redis.Z{Member: "test:1", Score: float64(now.Add(time.Second).UnixMilli())}).Err())
			require.NoError(t, client.Set(ctx, keys[2], "7", 0).Err())
			require.NoError(t, client.Set(ctx, metadataKey, "private-protobuf-not-exported", 10*time.Second).Err())
			require.NoError(t, client.ZAdd(ctx, keys[4], redis.Z{Member: metadataKey, Score: float64(now.Add(10 * time.Second).Unix())}).Err())
			reason := "automatic_asynq_consumer_consumer_absent"
			switch fault {
			case "operator-stop":
				reason = "operator_emergency_stop"
			case "unknown-stop":
				reason = "unknown_new_failure"
			case "consumer-lost-stop":
				reason = "automatic_permit_consumer_lost"
			}
			require.NoError(t, client.Set(ctx, keys[5], reason, 0).Err())
			require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: keys[15], ID: "1-0", Values: map[string]any{"event": "bootstrap"}}).Err())
			incidentID, err := client.XAdd(ctx, &redis.XAddArgs{Stream: keys[14], Values: map[string]any{"event": "trip", "reason": reason}}).Result()
			require.NoError(t, err)
			proof := systemHealthProof{
				WorkerID: "test:1", WorkerValue: workerValue, MembershipRevision: "7",
				ConsumerServerIDs: []string{serverID},
				Metadata:          map[string]string{metadataKey: redisCASDigest("private-protobuf-not-exported")},
			}
			args, err := systemProofArgs(proof)
			require.NoError(t, err)
			state := systemServiceState{
				Version: 1, Namespace: prefix, Epoch: uuid.NewString(), RuntimeSHA: "0123456789012345678901234567890123456789",
				WorkerID: proof.WorkerID, WorkerValue: proof.WorkerValue, MembershipRevision: proof.MembershipRevision,
				GuardFingerprint: "0123456789012345678901234567890123456789012345678901234567890123", Phase: "canary",
			}
			raw, err := json.Marshal(state)
			require.NoError(t, err)
			keys[18] = prefix + "system_commit:" + state.Epoch
			args = append(args, reason, "", "", "", "", string(raw), "sql-receipt-digest",
				`{"prior_guard_id":"","prior_control_id":"1-0"}`, incidentID, "1-0", "", "", incidentID)
			switch fault {
			case "membership":
				require.NoError(t, client.Set(ctx, keys[2], "8", 0).Err())
			case "metadata":
				require.NoError(t, client.Set(ctx, metadataKey, "changed-after-probe", 10*time.Second).Err())
			case "lease":
				require.NoError(t, client.ZAdd(ctx, keys[1], redis.Z{Member: "test:1", Score: float64(now.Add(-time.Second).UnixMilli())}).Err())
			case "trip":
				require.NoError(t, client.Set(ctx, keys[5], "operator_emergency_stop", 0).Err())
			case "old-work":
				require.NoError(t, client.HSet(ctx, keys[10], "old-request", "still-owned").Err())
			case "audit":
				require.NoError(t, client.Set(ctx, keys[13], "wrong-type", 0).Err())
			case "incarnation":
				require.NoError(t, client.HSet(ctx, keys[3], "test:1", "fp:different-incarnation").Err())
			case "original-audit-missing":
				args[16] = "0-0"
			case "original-audit-mismatch":
				wrongID, err := client.XAdd(ctx, &redis.XAddArgs{Stream: keys[14], Values: map[string]any{"event": "trip", "reason": "other"}}).Result()
				require.NoError(t, err)
				args[12], args[16] = wrongID, wrongID
			case "audit-tail-changed":
				require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{Stream: keys[14], Values: map[string]any{"event": "emergency_stop"}}).Err())
			case "observation-changed":
				require.NoError(t, client.Set(ctx, keys[9], "new-observation", 0).Err())
			case "required-changed":
				require.NoError(t, client.Set(ctx, keys[16], "new-recovery", 0).Err())
			}
			err = migrateSystemService.Run(ctx, client, keys, args...).Err()
			if fault != "none" {
				require.Error(t, err)
				require.EqualValues(t, 0, client.Exists(ctx, keys[6]).Val())
				require.EqualValues(t, 1, client.Exists(ctx, keys[5]).Val(), "failed migration cannot clear a stop")
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 0, client.Exists(ctx, keys[5]).Val())
			loaded, err := systemState(client.Get(ctx, keys[6]).Val())
			require.NoError(t, err)
			require.Equal(t, state, loaded)
			journal, err := client.XRange(ctx, keys[13], "-", "+").Result()
			require.NoError(t, err)
			require.Len(t, journal, 1)
			require.Equal(t, reason, journal[0].Values["original_trip"])
			require.Equal(t, incidentID, journal[0].Values["original_trip_audit_id"])
			require.NotContains(t, journal[0].Values, "private-protobuf-not-exported")
			// Lost acknowledgment/repeated migration is a changed-state error,
			// not an implicit success, duplicate audit or permission grant.
			require.Error(t, migrateSystemService.Run(ctx, client, keys, args...).Err())
			require.EqualValues(t, 1, client.XLen(ctx, keys[13]).Val())
			require.EqualValues(t, 0, client.Exists(ctx, prefix+"permit").Val())
		})
	}
}

func TestSystemServiceDoesNotRecoverUnknownOrHardStops(t *testing.T) {
	require.True(t, automaticServiceRecoverableTrip("automatic_asynq_consumer_consumer_absent"))
	for _, reason := range []string{
		"", "operator_emergency_stop", "normal_service_revoked", "automatic_permit_worker_lost",
		"automatic_permit_consumer_lost", "registry_incarnation_conflict", "new_unknown_failure", "data_integrity_failure",
	} {
		require.False(t, automaticServiceRecoverableTrip(reason), reason)
	}
}
