package feishusynccontrol

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"google.golang.org/protobuf/encoding/protowire"
)

// SharedQueueTaskProof authorizes consumption of an existing delivery, never a
// new grant or replay. Terminal SQL runs are acknowledged without Claim; only
// unclaimed, first-attempt non-Feishu runs may enter the normal execution path.
type SharedQueueTaskProof struct {
	TaskID      string `json:"task_id"`
	MessageSHA  string `json:"message_sha256"`
	SourceID    string `json:"source_id"`
	RunID       string `json:"run_id"`
	TenantID    uint64 `json:"tenant_id"`
	Status      string `json:"status"`
	Generation  int64  `json:"generation"`
	SourceType  string `json:"source_type"`
	Disposition string `json:"disposition"`
}

// Raw messages stay in the protected Redis audit namespace, not in the Web
// plan or logs. Hex field names and byte values preserve arbitrary Redis bytes.
type sharedQueueCheckpoint struct {
	Pause  string              `json:"pause"`
	IDs    []string            `json:"ids"`
	Fields []map[string][]byte `json:"fields"`
}

func checkpointBytes(q recoveryQueueSnapshot) ([]byte, error) {
	x := sharedQueueCheckpoint{Pause: q.pause, IDs: q.ids}
	for _, fields := range q.fields {
		out := map[string][]byte{}
		for k, v := range fields {
			out[hex.EncodeToString([]byte(k))] = []byte(v)
		}
		x.Fields = append(x.Fields, out)
	}
	return json.Marshal(x)
}

// ConfigureSharedQueueProof installs the authoritative SQL task-identity validator.
func (m *RecoveryManager) ConfigureSharedQueueProof(
	load func(context.Context, types.DataSourceSyncPayload) (SharedQueueTaskProof, error),
) {
	m.sharedQueueProof = load
}

// Bind the SQL proof to the exact signed message bytes, not a separate live
// Inspector read that could observe a different payload during reconciliation.
func sharedTaskPayload(raw string) ([]byte, error) {
	data := []byte(raw)
	var taskType, payload []byte
	for len(data) > 0 {
		num, wire, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, ErrRecoveryRejected
		}
		data = data[n:]
		if num == 1 || num == 2 {
			if wire != protowire.BytesType {
				return nil, ErrRecoveryRejected
			}
			value, size := protowire.ConsumeBytes(data)
			if size < 0 || (num == 1 && taskType != nil) || (num == 2 && payload != nil) {
				return nil, ErrRecoveryRejected
			}
			if num == 1 {
				taskType = value
			} else {
				payload = value
			}
			data = data[size:]
		} else {
			size := protowire.ConsumeFieldValue(num, wire, data)
			if size < 0 {
				return nil, ErrRecoveryRejected
			}
			data = data[size:]
		}
	}
	if string(taskType) != types.TypeDataSourceSync || len(payload) == 0 {
		return nil, ErrRecoveryRejected
	}
	return payload, nil
}

func (m *RecoveryManager) prepareSharedQueue(
	ctx context.Context, q recoveryQueueSnapshot,
) ([]SharedQueueTaskProof, []byte, error) {
	if m.sharedQueueProof == nil || len(q.ids) > 64 || len(q.ids) != len(q.fields) {
		return nil, nil, ErrRecoveryRejected
	}
	inspector := asynq.NewInspectorFromRedisClient(m.controller.Client)
	proofs := make([]SharedQueueTaskProof, 0, len(q.ids))
	for i, id := range q.ids {
		info, err := inspector.GetTaskInfo(types.QueueSync, id)
		if err != nil || info.Type != types.TypeDataSourceSync || info.State != asynq.TaskStatePending {
			return nil, nil, ErrRecoveryRejected
		}
		payload, err := sharedTaskPayload(q.fields[i]["msg"])
		if err != nil || !bytes.Equal(payload, info.Payload) {
			return nil, nil, ErrRecoveryRejected
		}
		var p types.DataSourceSyncPayload
		if json.Unmarshal(payload, &p) != nil || p.TenantID == 0 || p.DataSourceID == "" || p.SyncLogID == "" {
			return nil, nil, ErrRecoveryRejected
		}
		proof, err := m.sharedQueueProof(ctx, p)
		if err != nil || proof.SourceID != p.DataSourceID ||
			proof.RunID != p.SyncLogID || proof.TenantID != p.TenantID {
			return nil, nil, ErrRecoveryRejected
		}
		terminal := proof.Disposition == "acknowledge_terminal" &&
			(proof.Status == "success" || proof.Status == "partial" || proof.Status == "failed" ||
				proof.Status == "canceled" || proof.Status == "deferred")
		// An unknown connector cannot acquire execution permission through this
		// operational release. Production's blocked ordinary lane is GitLab.
		execute := proof.Disposition == "execute_original" && proof.SourceType == types.ConnectorTypeGitLab &&
			proof.Status == "running" && proof.Generation == 0
		if !terminal && !execute {
			return nil, nil, ErrRecoveryRejected
		}
		proof.TaskID, proof.MessageSHA = id, digestBytes(q.fields[i]["msg"])
		proofs = append(proofs, proof)
	}
	body, err := checkpointBytes(q)
	if err != nil || len(body) > 1<<20 {
		return nil, nil, ErrRecoveryRejected
	}
	return proofs, body, nil
}

// PlanSteadyWithSharedQueueRelease extends the existing signed normal policy.
// The original task IDs, payload bytes, SQL run identities and dispositions are
// reviewed as a bounded batch; the operation never recreates an old delivery.
func (m *RecoveryManager) PlanSteadyWithSharedQueueRelease(
	ctx context.Context, actor string, sources, reconcile []string, days int,
) (SteadyPlan, error) {
	p, err := m.PlanSteadyWithReconciliation(ctx, actor, sources, reconcile, days)
	if err != nil || p.ReleaseSharedQueue {
		return p, err
	}
	q, err := m.queue(ctx)
	if err != nil || q.digest != p.HistoricalQueueSHA256 || len(q.ids) != p.HistoricalPending {
		return SteadyPlan{}, ErrRecoveryRejected
	}
	proofs, body, err := m.prepareSharedQueue(ctx, q)
	if err != nil {
		return SteadyPlan{}, err
	}
	p.ReleaseSharedQueue = true
	p.SharedCheckpointID = uuid.NewString()
	p.SharedCheckpointSHA = digestBytes(string(body))
	p.SharedTasks = proofs
	return p, nil
}

func validSharedQueuePlan(p SteadyPlan) bool {
	if !p.ReleaseSharedQueue {
		return p.SharedCheckpointID == "" && p.SharedCheckpointSHA == "" && len(p.SharedTasks) == 0
	}
	if _, err := uuid.Parse(p.SharedCheckpointID); err != nil || !recoverySHA.MatchString(p.SharedCheckpointSHA) ||
		len(p.SharedTasks) != p.HistoricalPending || len(p.SharedTasks) > 64 {
		return false
	}
	seen := map[string]bool{}
	for _, task := range p.SharedTasks {
		if task.TaskID == "" || seen[task.TaskID] || !recoverySHA.MatchString(task.MessageSHA) ||
			task.SourceID == "" || task.RunID == "" || task.TenantID == 0 {
			return false
		}
		seen[task.TaskID] = true
	}
	return true
}

// releasedQueueSnapshot is an immutable incident checkpoint, explicitly not
// the live queue. New normal work may progress without invalidating old audit
// evidence. A missing/tampered checkpoint or renewed pause still fails closed.
func (m *RecoveryManager) releasedQueueSnapshot(ctx context.Context) (recoveryQueueSnapshot, bool, error) {
	var q recoveryQueueSnapshot
	raw, err := m.controller.Client.Get(ctx, m.controller.prefix+"recovery").Result()
	if err != nil {
		return q, false, nil
	}
	var state recoveryMode
	if json.Unmarshal([]byte(raw), &state) != nil || state.Steady == nil || !state.Steady.Plan.ReleaseSharedQueue {
		return q, false, nil
	}
	p := state.Steady.Plan
	// A new real incident owns a new physical queue snapshot and signatures;
	// its recovery must not inherit the previous lifecycle's released receipt.
	tripped, tripErr := m.controller.Client.Exists(ctx, m.controller.prefix+"trip").Result()
	if tripErr != nil {
		return q, true, ErrRecoveryRejected
	}
	if tripped != 0 {
		return q, false, nil
	}
	if state.Version != 3 || p.RuntimeSHA != m.controller.Config.SourceSHA ||
		p.WorkerID != m.controller.Config.WorkerID || p.Incarnation != m.controller.incarnation ||
		p.ControllerFingerprint != m.controller.fingerprint ||
		m.verifySteady(*state.Steady, time.Now().UnixMilli(), false) != nil {
		return q, true, ErrRecoveryRejected
	}
	paused, err := m.controller.Client.Exists(ctx, "asynq:{sync}:paused").Result()
	if err != nil || paused != 0 {
		return q, true, ErrRecoveryRejected
	}
	checkpointKey := m.controller.prefix + "shared_queue_checkpoint:" + p.SharedCheckpointID
	body, err := m.controller.Client.Get(ctx, checkpointKey).Result()
	var x sharedQueueCheckpoint
	if err != nil || digestBytes(body) != p.SharedCheckpointSHA || json.Unmarshal([]byte(body), &x) != nil ||
		len(x.IDs) != len(p.SharedTasks) || len(x.Fields) != len(x.IDs) {
		return q, true, ErrRecoveryRejected
	}
	q.ids, q.pause, q.digest = x.IDs, x.Pause, p.HistoricalQueueSHA256
	for i, fields := range x.Fields {
		out := map[string]string{}
		for k, v := range fields {
			key, decodeErr := hex.DecodeString(k)
			if decodeErr != nil {
				return q, true, ErrRecoveryRejected
			}
			out[string(key)] = string(v)
		}
		if x.IDs[i] != p.SharedTasks[i].TaskID || digestBytes(out["msg"]) != p.SharedTasks[i].MessageSHA {
			return q, true, ErrRecoveryRejected
		}
		q.fields = append(q.fields, out)
	}
	return q, true, nil
}

func sameSharedQueueRelease(a, b SteadyPlan) bool {
	return a.ReleaseSharedQueue == b.ReleaseSharedQueue && a.SharedCheckpointID == b.SharedCheckpointID &&
		a.SharedCheckpointSHA == b.SharedCheckpointSHA && reflect.DeepEqual(a.SharedTasks, b.SharedTasks)
}
