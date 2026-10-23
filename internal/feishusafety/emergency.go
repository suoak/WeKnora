package feishusafety

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

// EmergencyBlock is a provider-independent, stop-only operation. It does not
// need a model or verified budget, cannot clear a latch, and never deletes tasks.
// Callers must separately verify active execution fencing; queue pause is not drain.
func EmergencyBlock(ctx context.Context, client redis.UniversalClient, namespace string) error {
	if client == nil || namespace == "" || len(namespace) > 100 || strings.ContainsAny(namespace, "{} \t\r\n") {
		return fmt.Errorf("%w: invalid emergency scope", ErrClosed)
	}
	prefix := "feishu:safety:{" + namespace + "}:"
	blockErr := redis.NewScript(`
redis.call('SET',KEYS[1],'operator_emergency_stop','NX')
redis.call('DEL',KEYS[2])
redis.call('XADD',KEYS[3],'MAXLEN','~',1000,'*','event','emergency_stop')
return 1`).
		Run(ctx, client,
			[]string{prefix + "trip", prefix + "permit", prefix + "audit"}).
		Err()
	// Queue key is in a different Redis Cluster slot, so do not pretend both
	// actions are atomic. Return either failure and retain whichever stop succeeded.
	pauseErr := client.Set(ctx, "asynq:{sync}:paused", "feishu_emergency_stop", 0).Err()
	if blockErr != nil || pauseErr != nil {
		return fmt.Errorf("emergency block=%v queue_pause=%v", blockErr, pauseErr)
	}
	return nil
}
