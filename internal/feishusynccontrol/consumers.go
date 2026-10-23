package feishusynccontrol

import (
	"context"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// SyncConsumers reads Asynq's actual Redis-backed server heartbeat registry.
// It does not turn the presence of a controller into proof of a live consumer.
// Asynq's own heartbeat/expiry interval bounds detection of consumer-only loss;
// independent controller lease loss is detected after two seconds.
func SyncConsumers(client redis.UniversalClient) func(context.Context) ([]string, error) {
	inspector := asynq.NewInspectorFromRedisClient(client)
	return func(ctx context.Context) ([]string, error) {
		if err := client.Ping(ctx).Err(); err != nil {
			return nil, &probeFailure{stage: "redis_ping", cause: err}
		}
		servers, err := inspector.Servers()
		if err != nil {
			return nil, &probeFailure{stage: "asynq_inspection", cause: err}
		}
		var ids []string
		for _, server := range servers {
			if server.Queues["sync"] > 0 {
				if server.Status != "active" || server.Concurrency < 1 {
					return nil, &probeFailure{stage: "asynq_consumer", cause: errConsumerInactive}
				}
				ids = append(ids, fmt.Sprintf("%s:%d", server.Host, server.PID))
			}
		}
		if len(ids) == 0 {
			return nil, &probeFailure{stage: "asynq_consumer", cause: errConsumerAbsent}
		}
		return ids, nil
	}
}
