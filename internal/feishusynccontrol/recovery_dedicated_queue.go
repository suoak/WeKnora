package feishusynccontrol

import (
	"context"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
)

const dedicatedRecoveryMaxPending = 1024

// This barrier belongs to an explicit reviewed reset/canary, not the automatic
// controller stop path. Keep it stable while preparing the signed plan even
// when a previous incident has already released its queue.
const dedicatedRecoveryPause = "feishu_recovery_canary"

// Normal service quarantines Feishu on its own lane. Its incident recovery
// must neither require a shared-queue pause nor adopt an old shared checkpoint.
// Historical continuity/reconciliation still uses queue() and its old proof.
func (m *RecoveryManager) incidentQueue(ctx context.Context) (recoveryQueueSnapshot, error) {
	if !m.controller.Config.DedicatedSyncLane {
		return m.queue(ctx)
	}
	normal, err := m.NormalServiceActive(ctx)
	if err != nil {
		return recoveryQueueSnapshot{}, err
	}
	if !normal {
		return m.queue(ctx)
	}
	c := m.controller.Client
	q := recoveryQueueSnapshot{name: "approved_sync"}
	const prefix = "asynq:{approved_sync}:"
	q.pause, err = c.Get(ctx, prefix+"paused").Result()
	if err != nil || (q.pause != "feishu_safety_stop" && q.pause != dedicatedRecoveryPause) {
		return q, ErrRecoveryRejected
	}
	for _, suffix := range []string{"active", "retry", "scheduled"} {
		var count int64
		if suffix == "active" {
			count, err = c.LLen(ctx, prefix+suffix).Result()
		} else {
			count, err = c.ZCard(ctx, prefix+suffix).Result()
		}
		if err != nil || count != 0 {
			return q, ErrRecoveryRejected
		}
	}
	// Archived jobs cannot execute. Keep them untouched, including expired
	// historical failures; requiring their deletion would prevent recovery.
	q.ids, err = c.LRange(ctx, prefix+"pending", 0, dedicatedRecoveryMaxPending).Result()
	if err != nil || len(q.ids) > dedicatedRecoveryMaxPending {
		return q, ErrRecoveryRejected
	}
	var hashes []map[string]string
	bytes := 0
	for _, id := range q.ids {
		if id == "" || !utf8.ValidString(id) {
			return q, ErrRecoveryRejected
		}
		fields, err := c.HGetAll(ctx, prefix+"t:"+id).Result()
		if err != nil || len(fields) == 0 || len(fields) > 64 {
			return q, ErrRecoveryRejected
		}
		q.fields = append(q.fields, fields)
		hash := map[string]string{}
		for key, value := range fields {
			bytes += len(key) + len(value)
			if len(key) > 1024 || len(value) > 1<<20 || bytes > 16<<20 {
				return q, ErrRecoveryRejected
			}
			hash[hex.EncodeToString([]byte(key))] = digestBytes(value)
		}
		hashes = append(hashes, hash)
	}
	q.digest, err = RecoveryDigest([]any{q.name, digestBytes(q.pause), q.ids, hashes})
	return q, err
}

func recoveryQueueMatches(q recoveryQueueSnapshot, p RecoveryPlan) bool {
	return q.name == p.QueueName && q.digest == p.QueueSHA256 && len(q.ids) == p.QueuePendingCount
}

func recoveryIncidentScript(q recoveryQueueSnapshot, body string) *redis.Script {
	cas := recoveryQueueCAS
	if q.name == "approved_sync" {
		cas = strings.Replace(cas, " or\n redis.call('ZCARD','asynq:{sync}:archived')~=0", "", 1)
		cas = strings.ReplaceAll(cas, "asynq:{sync}:", "asynq:{approved_sync}:")
		body = strings.ReplaceAll(body, "asynq:{sync}:", "asynq:{approved_sync}:")
	}
	return redis.NewScript(cas + body)
}
