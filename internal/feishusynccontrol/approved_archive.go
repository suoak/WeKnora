package feishusynccontrol

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

type approvedArchiveProof struct {
	ids      []string
	pending  []string
	messages map[string]string
}

func (p *approvedArchiveProof) digest() string {
	if p == nil || len(p.ids) == 0 {
		return ""
	}
	var rows [][2]string
	for _, id := range p.ids {
		rows = append(rows, [2]string{id, digestBytes(p.messages[id])})
	}
	hash, _ := RecoveryDigest(rows)
	return hash
}

func (p *approvedArchiveProof) pendingDigest() string {
	if p == nil || len(p.pending) == 0 {
		return ""
	}
	var rows [][2]string
	for _, id := range p.pending {
		rows = append(rows, [2]string{id, digestBytes(p.messages[id])})
	}
	hash, _ := RecoveryDigest(rows)
	return hash
}

// ConfigureArchivedTerminal proves immutable SQL terminal state, independently
// of Redis's archive label. Without this loader, archives still fail closed.
func (m *RecoveryManager) ConfigureArchivedTerminal(
	load func(context.Context, []types.DataSourceSyncPayload) (bool, error),
) {
	m.archivedTerminal = load
}

// All SQL work occurs outside the controller mutex so database latency cannot
// prevent permit renewal. The immutable terminal decisions are bound to exact
// Redis messages, rechecked and WATCHed at policy activation; nothing is deleted
// or replayed. Separately signed pending-terminal proof permits only normal
// acknowledgment of immutable SQL terminal deliveries, without Claim or fetch.
// Live retries/scheduled/active tasks still prevent replacement.
func (m *RecoveryManager) prepareApprovedArchive(ctx context.Context) (*approvedArchiveProof, error) {
	base := "asynq:{" + ApprovedSyncQueue + "}:"
	ids, err := m.controller.Client.ZRange(ctx, base+"archived", 0, -1).Result()
	if err != nil || len(ids) > 1000 {
		return nil, ErrRecoveryRejected
	}
	pending, err := m.controller.Client.LRange(ctx, base+"pending", 0, -1).Result()
	if err != nil || len(pending) > 64 {
		return nil, ErrRecoveryRejected
	}
	proof := &approvedArchiveProof{ids: ids, pending: pending, messages: map[string]string{}}
	if len(ids)+len(pending) == 0 {
		return proof, nil
	}
	if m.archivedTerminal == nil {
		return nil, ErrRecoveryRejected
	}
	inspector := asynq.NewInspectorFromRedisClient(m.controller.Client)
	payloads := make([]types.DataSourceSyncPayload, 0, len(ids)+len(pending))
	for i, id := range append(append([]string(nil), ids...), pending...) {
		key := base + "t:" + id
		before, err := m.controller.Client.HGet(ctx, key, "msg").Result()
		if err != nil || proof.messages[id] != "" {
			return nil, ErrRecoveryRejected
		}
		info, err := inspector.GetTaskInfo(ApprovedSyncQueue, id)
		expectedState := asynq.TaskStateArchived
		if i >= len(ids) {
			expectedState = asynq.TaskStatePending
		}
		if err != nil || info.Type != types.TypeDataSourceSync || info.State != expectedState {
			return nil, ErrRecoveryRejected
		}
		after, err := m.controller.Client.HGet(ctx, key, "msg").Result()
		if err != nil || before != after {
			return nil, ErrRecoveryRejected
		}
		var payload types.DataSourceSyncPayload
		if json.Unmarshal(info.Payload, &payload) != nil || payload.DataSourceID == "" ||
			payload.SyncLogID == "" || payload.TenantID == 0 {
			return nil, ErrRecoveryRejected
		}
		payloads = append(payloads, payload)
		proof.messages[id] = after
	}
	terminal, err := m.archivedTerminal(ctx, payloads)
	if err != nil || !terminal {
		return nil, ErrRecoveryRejected
	}
	return proof, nil
}

func (m *RecoveryManager) approvedQueueQuiescent(ctx context.Context, proof *approvedArchiveProof) error {
	if proof == nil {
		return ErrRecoveryRejected
	}
	base := "asynq:{" + ApprovedSyncQueue + "}:"
	if n, err := m.controller.Client.LLen(ctx, base+"active").Result(); err != nil || n != 0 {
		return ErrRecoveryRejected
	}
	pending, err := m.controller.Client.LRange(ctx, base+"pending", 0, -1).Result()
	if err != nil || !slices.Equal(pending, proof.pending) {
		return ErrRecoveryRejected
	}
	for _, name := range []string{"retry", "scheduled"} {
		n, err := m.controller.Client.ZCard(ctx, base+name).Result()
		if err != nil || n != 0 {
			return ErrRecoveryRejected
		}
	}
	ids, err := m.controller.Client.ZRange(ctx, base+"archived", 0, -1).Result()
	if err != nil || !slices.Equal(ids, proof.ids) {
		return ErrRecoveryRejected
	}
	for _, id := range append(append([]string(nil), ids...), pending...) {
		msg, err := m.controller.Client.HGet(ctx, base+"t:"+id, "msg").Result()
		if err != nil || msg != proof.messages[id] {
			return ErrRecoveryRejected
		}
	}
	return nil
}
