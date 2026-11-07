package feishusynccontrol

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func startProbeConsumer(t *testing.T, f *controlFixture) {
	t.Helper()
	server := asynq.NewServer(asynq.RedisClientOpt{Addr: f.server.Addr()}, asynq.Config{
		Concurrency: 1, Queues: map[string]int{"sync": 1, ApprovedSyncQueue: 1},
		ShutdownTimeout: time.Second,
	})
	require.NoError(t, server.Start(asynq.NewServeMux()))
	t.Cleanup(server.Shutdown)
}

func TestConsumerProbeDoesNotBorrowAnUnrelatedControllerDeadline(t *testing.T) {
	f := newControlFixture(t)
	startProbeConsumer(t, f)
	require.Eventually(t, func() bool {
		return f.client.ZCard(context.Background(), "asynq:servers").Val() == 1
	}, time.Second, time.Millisecond)
	client, transport := NewControlClient(f.client)
	t.Cleanup(func() { _ = client.Close() })
	step, cancelStep := context.WithCancel(context.Background())
	cancelStep()
	transport.begin(step)
	defer transport.finish()
	// A canceled controller cycle must not replace the still-live source's
	// inspection deadline, including Asynq's internal Background GETs.
	source, cancelSource := context.WithTimeout(context.Background(), time.Second)
	defer cancelSource()
	ids, err := ApprovedConsumers(client)(source)
	require.NoError(t, err)
	host, err := os.Hostname()
	require.NoError(t, err)
	require.Equal(t, []string{fmt.Sprintf("%s:%d", host, os.Getpid())}, ids)
}

type consumerReadFailureHook struct{ cause error }

func (h consumerReadFailureHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return next(ctx, network, address)
	}
}
func (h consumerReadFailureHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h consumerReadFailureHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "get" && strings.HasPrefix(fmt.Sprint(cmd.Args()[1]), "asynq:servers:") {
			return h.cause
		}
		return next(ctx, cmd)
	}
}

func TestConsumerProbePreservesSkippedMetadataReadFailures(t *testing.T) {
	f := newControlFixture(t)
	startProbeConsumer(t, f)
	require.Eventually(t, func() bool {
		return f.client.ZCard(context.Background(), "asynq:servers").Val() == 1
	}, time.Second, time.Millisecond)
	failed := errors.New("synthetic metadata transport failure")
	f.client.AddHook(consumerReadFailureHook{cause: failed})
	_, err := ApprovedConsumers(f.client)(context.Background())
	require.ErrorIs(t, err, failed)
	require.NotErrorIs(t, err, errConsumerAbsent)
}
