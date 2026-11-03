package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

func sharedQueueFixture(t *testing.T) (*steadyFixture, map[string]SharedQueueTaskProof) {
	t.Helper()
	f := newSteadyFixture(t)
	ctx := context.Background()
	ids, err := f.r.LRange(ctx, "asynq:{sync}:pending", 0, -1).Result()
	require.NoError(t, err)
	for _, id := range ids {
		require.NoError(t, f.r.Del(ctx, "asynq:{sync}:t:"+id).Err())
	}
	require.NoError(t, f.r.Del(ctx, "asynq:{sync}:pending").Err())
	proofs := map[string]SharedQueueTaskProof{}
	client := asynq.NewClientFromRedisClient(f.r)
	for i := 0; i < 6; i++ {
		p := types.DataSourceSyncPayload{
			DataSourceID: uuid.NewString(), SyncLogID: uuid.NewString(), TenantID: uint64(i + 1),
		}
		body, marshalErr := json.Marshal(p)
		require.NoError(t, marshalErr)
		_, enqueueErr := client.Enqueue(asynq.NewTask(types.TypeDataSourceSync, body),
			asynq.Queue(types.QueueSync), asynq.TaskID(p.SyncLogID))
		require.NoError(t, enqueueErr)
		proof := SharedQueueTaskProof{
			SourceID: p.DataSourceID, RunID: p.SyncLogID, TenantID: p.TenantID,
			Status: "canceled", Generation: 1, Disposition: "acknowledge_terminal",
		}
		if i < 2 {
			proof.Status, proof.Generation = "running", 0
			proof.SourceType, proof.Disposition = "gitlab", "execute_original"
		}
		proofs[p.SyncLogID] = proof
	}
	f.m.ConfigureSharedQueueProof(func(_ context.Context, p types.DataSourceSyncPayload) (SharedQueueTaskProof, error) {
		proof, ok := proofs[p.SyncLogID]
		if !ok {
			return proof, ErrRecoveryRejected
		}
		return proof, nil
	})
	f.activate(t)
	return f, proofs
}

func TestSharedQueueReleaseRetainsBytesAndAllowsNormalProgress(t *testing.T) {
	f, _ := sharedQueueFixture(t)
	ctx := context.Background()
	before, err := f.m.queue(ctx)
	require.NoError(t, err)
	p, err := f.m.PlanSteadyWithSharedQueueRelease(ctx, "operator", f.ids, nil, 30)
	require.NoError(t, err)
	require.Len(t, p.SharedTasks, 6)
	require.NoError(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
	require.Zero(t, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
	for i, id := range before.ids {
		fields, readErr := f.r.HGetAll(ctx, "asynq:{sync}:t:"+id).Result()
		require.NoError(t, readErr)
		require.Equal(t, before.fields[i], fields, "activation must not rewrite a payload or execution")
	}
	checkpointKey := f.c.prefix + "shared_queue_checkpoint:" + p.SharedCheckpointID
	checkpoint := f.r.Get(ctx, checkpointKey).Val()
	require.Equal(t, p.SharedCheckpointSHA, digestBytes(checkpoint))
	// Simulate normal acknowledgment and new enqueue; the immutable incident
	// checkpoint must not be mistaken for the live queue's changing digest.
	id, err := f.r.RPop(ctx, "asynq:{sync}:pending").Result()
	require.NoError(t, err)
	require.NoError(t, f.r.Del(ctx, "asynq:{sync}:t:"+id).Err())
	require.NoError(t, f.r.LPush(ctx, "asynq:{sync}:pending", "new-normal-delivery").Err())
	require.NoError(t, f.c.checkRecoveryPolicy(ctx), "policy after normal queue progress")
	stepErr := f.c.Step(ctx)
	require.NoError(t, stepErr, "trip=%s", f.r.Get(ctx, f.c.prefix+"trip").Val())
	after, err := f.m.queue(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, checkpoint, f.r.Get(ctx, checkpointKey).Val())
	status, err := f.m.Status(ctx, f.p.Facts.SourceID)
	require.NoError(t, err)
	require.Equal(t, true, status["permit_ready"])
	require.Equal(t, true, status["shared_queue_reconciled"])
	require.Equal(t, false, status["sync_paused"])
	// An ordinary source-policy revocation retains the separately signed
	// checkpoint and never replays its historical messages.
	revoke, err := f.m.PlanSteady(ctx, "operator", f.ids[:2], 30)
	require.NoError(t, err)
	require.True(t, sameSharedQueueRelease(revoke, p))
	require.NoError(t, f.m.ActivateSteady(ctx, revoke, f.sign(t, revoke), "operator"))
	require.NoError(t, f.c.Step(ctx))
}

func TestSharedQueueReleaseRejectsChangedEvidence(t *testing.T) {
	for _, fault := range []string{"unsigned", "payload", "sql", "live_feishu", "claimed", "active", "trip", "actor"} {
		t.Run(fault, func(t *testing.T) {
			f, proofs := sharedQueueFixture(t)
			ctx := context.Background()
			p, err := f.m.PlanSteadyWithSharedQueueRelease(ctx, "operator", f.ids, nil, 30)
			require.NoError(t, err)
			a := f.sign(t, p)
			switch fault {
			case "unsigned":
				a.Signature = "00"
			case "payload":
				require.NoError(t, f.r.HSet(ctx, "asynq:{sync}:t:"+p.SharedTasks[0].TaskID, "msg", "changed").Err())
			case "sql", "live_feishu", "claimed":
				f.m.ConfigureSharedQueueProof(func(
					_ context.Context, payload types.DataSourceSyncPayload,
				) (SharedQueueTaskProof, error) {
					proof := proofs[payload.SyncLogID]
					if fault == "claimed" {
						return proof, errors.New("unavailable or claimed")
					}
					proof.Status, proof.Generation, proof.Disposition = "running", 0, "execute_original"
					if fault == "live_feishu" {
						proof.SourceType = "feishu_drive"
					}
					return proof, nil
				})
			case "active":
				require.NoError(t, f.r.LPush(ctx, "asynq:{sync}:active", "concurrent-worker").Err())
			case "trip":
				require.NoError(t, f.c.Guard.Trip(ctx, "new_incident"))
			case "actor":
				a.ActorID = "other"
			}
			require.Error(t, f.m.ActivateSteady(ctx, p, a, "operator"))
			require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
			require.Zero(t, f.r.Exists(ctx, f.c.prefix+"shared_queue_checkpoint:"+p.SharedCheckpointID).Val())
		})
	}
}

func TestSharedQueueReleaseCheckpointAndNewSafetyStopFailClosed(t *testing.T) {
	for _, fault := range []string{"checkpoint_missing", "checkpoint_tampered", "expired", "pause"} {
		t.Run(fault, func(t *testing.T) {
			f, _ := sharedQueueFixture(t)
			ctx := context.Background()
			p, err := f.m.PlanSteadyWithSharedQueueRelease(ctx, "operator", f.ids, nil, 30)
			require.NoError(t, err)
			require.NoError(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
			key := f.c.prefix + "shared_queue_checkpoint:" + p.SharedCheckpointID
			switch fault {
			case "checkpoint_missing":
				require.NoError(t, f.r.Del(ctx, key).Err())
			case "checkpoint_tampered":
				require.NoError(t, f.r.Set(ctx, key, "changed", 0).Err())
			case "expired":
				require.NoError(t, f.r.Del(ctx, f.c.prefix+"steady_health").Err())
			case "pause":
				require.NoError(t, f.r.Set(ctx, "asynq:{sync}:paused", "operator_pause", 0).Err())
			}
			require.Error(t, f.c.Step(ctx))
			require.EqualValues(t, 1, f.r.Exists(ctx, f.c.prefix+"trip").Val())
			require.EqualValues(t, 1, f.r.Exists(ctx, "asynq:{sync}:paused").Val())
			require.Error(t, f.c.Guard.Check(feishusafety.WithScope(ctx, f.ids[0])))
		})
	}
}

func TestSharedQueueReleaseRejectsSnapshotPayloadMismatch(t *testing.T) {
	f, _ := sharedQueueFixture(t)
	ctx := context.Background()
	q, err := f.m.queue(ctx)
	require.NoError(t, err)
	raw := []byte(q.fields[0]["msg"])
	_, err = sharedTaskPayload(string(raw))
	require.NoError(t, err)
	duplicate := protowire.AppendTag(append([]byte(nil), raw...), 2, protowire.BytesType)
	duplicate = protowire.AppendBytes(duplicate, []byte(`{}`))
	_, err = sharedTaskPayload(string(duplicate))
	require.Error(t, err, "ambiguous repeated payload must reject")
	_, err = sharedTaskPayload(string([]byte{0xff}))
	require.Error(t, err, "malformed protobuf must reject")
	changed := protowire.AppendTag(nil, 1, protowire.BytesType)
	changed = protowire.AppendString(changed, types.TypeDataSourceSync)
	changed = protowire.AppendTag(changed, 2, protowire.BytesType)
	changed = protowire.AppendBytes(changed, []byte(`{"data_source_id":"different"}`))
	q.fields[0]["msg"] = string(changed)
	_, _, err = f.m.prepareSharedQueue(ctx, q)
	require.Error(t, err, "SQL proof must bind the signed bytes, not another Inspector read")
}

func TestSharedQueueReleaseOutboundRequiresExactTenantAndCurrentClaim(t *testing.T) {
	f, _ := sharedQueueFixture(t)
	ctx := context.Background()
	p, err := f.m.PlanSteadyWithSharedQueueRelease(ctx, "operator", f.ids, nil, 30)
	require.NoError(t, err)
	require.NoError(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
	id := f.ids[1]
	run := uuid.NewString()
	claimLive := true
	f.m.facts = func(ctx context.Context, source string) (RecoveryFacts, error) {
		facts := f.facts[source]
		if execution, ok := types.SyncExecutionFromContext(ctx); ok {
			facts.Eligible = claimLive && execution.SyncLogID == run && execution.Generation == 7
		}
		return facts, nil
	}
	bound, _, err := BindApprovedSource(ctx, id, f.facts[id].ConfigSHA256)
	require.NoError(t, err)
	bound = feishusafety.WithScope(bound, id)
	bound = feishusafety.WithRecoveryExecution(bound, f.facts[id].ConfigSHA256, run, 7)
	calls := 0
	transport := feishusafety.Transport{
		Embedding: true,
		Base: continuityRoundTripper(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{
				StatusCode: 200, Header: http.Header{},
				Body: io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		}),
	}
	call := func(work context.Context) error {
		r, err := http.NewRequestWithContext(work, http.MethodPost, "https://gateway.invalid/v1/embeddings",
			strings.NewReader(`{"input":["approved synthetic input"]}`))
		require.NoError(t, err)
		response, err := transport.RoundTrip(r)
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		return err
	}
	require.Error(t, call(bound))
	require.Zero(t, calls)
	wrong := types.WithSyncExecution(bound, types.SyncExecution{
		DataSourceID: id, SyncLogID: run, TenantID: 999, Generation: 7,
	})
	require.Error(t, call(wrong))
	require.Zero(t, calls)
	valid := types.WithSyncExecution(bound, types.SyncExecution{
		DataSourceID: id, SyncLogID: run, TenantID: f.authority[id].TenantID, Generation: 7,
	})
	require.NoError(t, call(valid))
	require.Equal(t, 1, calls)
	claimLive = false
	require.Error(t, call(valid))
	require.Equal(t, 1, calls, "released claims never reach the transport")
	status, err := f.m.Status(ctx, f.recoveryFixture.facts.SourceID)
	require.NoError(t, err)
	stateJSON, err := json.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(stateJSON), "managed_sources", "anchor-only status must not disclose other tenants")
	require.NotContains(t, string(stateJSON), f.authority[id].KBID)
}

// This is the actual check script from the previously validated 50712d3 reader.
// Rollback must reject the new receipt rather than silently opening its lane.
func TestSharedQueueReleaseOldReaderRejectsCurrentReceipt(t *testing.T) {
	f, _ := sharedQueueFixture(t)
	ctx := context.Background()
	p, err := f.m.PlanSteadyWithSharedQueueRelease(ctx, "operator", f.ids, nil, 30)
	require.NoError(t, err)
	require.NoError(t, f.m.ActivateSteady(ctx, p, f.sign(t, p), "operator"))
	id := f.ids[0]
	work, approved, err := BindApprovedSource(ctx, id, f.facts[id].ConfigSHA256)
	require.NoError(t, err)
	require.True(t, approved)
	require.NoError(t, f.c.Guard.Check(feishusafety.WithScope(work, id)))
	oldReader := redis.NewScript(`
if redis.call('EXISTS',KEYS[2]) == 1 then return redis.error_reply('SAFETY_TRIPPED') end
if redis.call('GET',KEYS[1]) ~= ARGV[1] or redis.call('PTTL',KEYS[1]) <= 0 then
 return redis.error_reply('SAFETY_NO_PERMIT')
end
local raw=redis.call('GET',KEYS[3])
local required=redis.call('GET',KEYS[4])
if (required or (ARGV[7]=='true' or ARGV[7]=='1')) and not raw then return
redis.error_reply('RECOVERY_MISSING') end
if raw then
 local s=cjson.decode(raw)
 if not required or required~=s.plan_sha256 then return redis.error_reply('RECOVERY_BINDING_MISSING') end
 local now=redis.call('TIME'); local ms=now[1]*1000+math.floor(now[2]/1000)
 if (s.version~=1 and not(s.version==2 and s.steady_policy)) or
  not s.expires_ms or s.expires_ms<=ms then return redis.error_reply('RECOVERY_EXPIRED') end
 if s.steady_policy and (redis.call('GET',KEYS[5])~=s.continuity_id or
  redis.call('PTTL',KEYS[5])<=0) then return redis.error_reply('STEADY_HEALTH_EXPIRED') end
 if s.phase=='approved_sources' then
  local entry=s.approved_sources and s.approved_sources[ARGV[2]]
  if not entry or entry.config_sha256~=ARGV[4] or s.continuity_id~=ARGV[3] or
   (ARGV[8]~='true' and ARGV[8]~='1') then return redis.error_reply('APPROVED_SOURCE_DENIED') end
 elseif s.source_id~=ARGV[2] or s.config_sha256~=ARGV[4] then return
redis.error_reply('RECOVERY_SOURCE_DENIED') end
 if s.phase=='approved_sources' then
  -- Live authority was checked by the server validator before this atomic gate.
 elseif s.phase=='admit' then
  if s.plan_id~=ARGV[3] or ARGV[5]~='' then return redis.error_reply('RECOVERY_ADMISSION_DENIED') end
 elseif s.phase=='running' then
  if s.plan_id~=ARGV[3] or s.run_id~=ARGV[5] or tostring(s.generation)~=ARGV[6] then
   return redis.error_reply('RECOVERY_RUN_DENIED')
  end
 else return redis.error_reply('RECOVERY_FROZEN') end
end
return 1`)
	err = oldReader.Run(ctx, f.r, []string{
		f.c.Guard.PermitKey(), f.c.prefix + "trip",
		f.c.prefix + "recovery", f.c.prefix + "recovery_required", f.c.prefix + "steady_health",
	},
		f.c.Guard.Fingerprint(), id, p.ID, f.facts[id].ConfigSHA256, "", 0, true, true).Err()
	require.ErrorContains(t, err, "RECOVERY_EXPIRED")
}
