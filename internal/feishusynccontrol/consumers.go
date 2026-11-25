package feishusynccontrol

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// SyncConsumers reads Asynq's actual Redis-backed server heartbeat registry.
// It does not turn the presence of a controller into proof of a live consumer.
// Asynq's own heartbeat/expiry interval bounds detection of consumer-only loss;
// independent controller lease loss is detected after two seconds.
func SyncConsumers(client redis.UniversalClient) func(context.Context) ([]string, error) {
	return syncConsumersForQueues(client, false)
}

// ApprovedConsumers attests the actual additional consumer lane, rather than
// assuming a healthy shared sync consumer also subscribes to continuation.
func ApprovedConsumers(client redis.UniversalClient) func(context.Context) ([]string, error) {
	return syncConsumersForQueues(client, true)
}

func syncConsumersForQueues(
	client redis.UniversalClient,
	approved bool,
) func(context.Context) ([]string, error) {
	return func(ctx context.Context) (resultIDs []string, probeErr error) {
		ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		evidence := newConsumerEvidence(ctx, client, approved)
		defer func() {
			evidence.ElapsedMS = time.Since(evidence.started).Milliseconds()
			evidence.ProbeClass = consumerReadClass(probeErr)
		}()
		if err := client.Ping(ctx).Err(); err != nil {
			return nil, &probeFailure{stage: "redis_ping", cause: err}
		}
		probe := &consumerProbeRedis{UniversalClient: client, ctx: ctx, evidence: evidence}
		inspector := asynq.NewInspectorFromRedisClient(probe)
		servers, err := inspector.Servers()
		if err == nil {
			err = probe.failure
		}
		if err != nil {
			return nil, &probeFailure{stage: "asynq_inspection", cause: err}
		}
		var ids []string
		for _, server := range servers {
			if approved && server.Queues[ApprovedSyncQueue] > 0 && server.Queues["sync"] < 1 {
				return nil, &probeFailure{stage: "asynq_consumer", cause: errConsumerInactive}
			}
			if server.Queues["sync"] > 0 {
				if approved && server.Queues[ApprovedSyncQueue] < 1 {
					return nil, ErrNotReady
				}
				if server.Status != "active" || server.Concurrency < 1 {
					return nil, &probeFailure{stage: "asynq_consumer", cause: errConsumerInactive}
				}
				ids = append(ids, fmt.Sprintf("%s:%d", server.Host, server.PID))
				if id, e := uuid.Parse(server.ID); e == nil && id.String() == server.ID && len(evidence.ConsumerServerIDs) < consumerEvidenceLimit {
					evidence.ConsumerServerIDs = append(evidence.ConsumerServerIDs, server.ID)
				}
			}
		}
		if len(ids) == 0 {
			return nil, &probeFailure{stage: "asynq_consumer", cause: errConsumerAbsent}
		}
		return ids, nil
	}
}

// Asynq's inspector issues Background calls and silently skips failed metadata
// GETs. Keep every read on this probe's caller deadline: borrowing another
// controller Step's context can cancel a healthy source's inspection midway.
// Remember skipped read errors so an incomplete registry is never attested.
type consumerProbeRedis struct {
	redis.UniversalClient
	ctx      context.Context
	failure  error
	evidence *consumerEvidence
}

func (c *consumerProbeRedis) remember(err error) {
	if err != nil && err != redis.Nil && !redis.HasErrorPrefix(err, "NOSCRIPT") && c.failure == nil {
		c.failure = err
	}
}

func (c *consumerProbeRedis) Get(_ context.Context, key string) *redis.StringCmd {
	result := c.UniversalClient.Get(c.ctx, key)
	if c.evidence != nil {
		c.evidence.read(key, result.Val(), result.Err())
	}
	c.remember(result.Err())
	return result
}

func (c *consumerProbeRedis) HVals(_ context.Context, key string) *redis.StringSliceCmd {
	result := c.UniversalClient.HVals(c.ctx, key)
	c.remember(result.Err())
	return result
}

func (c *consumerProbeRedis) Eval(_ context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	result := c.UniversalClient.Eval(c.ctx, script, keys, args...)
	c.remember(result.Err())
	return result
}

func (c *consumerProbeRedis) EvalSha(_ context.Context, sha string, keys []string, args ...interface{}) *redis.Cmd {
	result := c.UniversalClient.EvalSha(c.ctx, sha, keys, args...)
	c.remember(result.Err())
	return result
}
