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
			return nil, err
		}
		servers, err := inspector.Servers()
		if err != nil {
			return nil, err
		}
		var ids []string
		for _, server := range servers {
			if server.Queues["sync"] > 0 {
				if server.Status != "active" || server.Concurrency < 1 {
					return nil, fmt.Errorf("sync consumer not active")
				}
				ids = append(ids, fmt.Sprintf("%s:%d", server.Host, server.PID))
			}
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("no active sync consumer")
		}
		return ids, nil
	}
}
