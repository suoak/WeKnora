package feishusynccontrol

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// These tests exercise the driver's real health and closed-wait paths. They do
// not manufacture a SQL epoch/publication receipt or claim successful recovery.
// Every fixture owns its Redis process; no FLUSH or shared test-site access.
func nativeAutomaticService(t *testing.T) (*automaticService, *redis.Client, asynq.RedisClientOpt) {
	t.Helper()
	executable := os.Getenv("FEISHU_SAFETY_REDIS_EXECUTABLE")
	if executable == "" {
		t.Skip("requires explicit owned Redis executable for native Asynq isolation")
	}
	dir, err := os.MkdirTemp("", "knowhub-driver-redis-")
	require.NoError(t, err)
	socket := filepath.Join(dir, "redis.sock")
	command := exec.Command(
		executable,
		"--port",
		"0",
		"--unixsocket",
		socket,
		"--unixsocketperm",
		"700",
		"--save",
		"",
		"--appendonly",
		"no",
		"--loglevel",
		"warning",
	)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	require.NoError(t, command.Start())
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	client := redis.NewClient(
		&redis.Options{
			Network:      "unix",
			Addr:         socket,
			MaxRetries:   -1,
			DialTimeout:  200 * time.Millisecond,
			ReadTimeout:  200 * time.Millisecond,
			WriteTimeout: 200 * time.Millisecond,
		},
	)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = client.ShutdownNoSave(ctx).Err()
		_ = client.Close()
		select {
		case <-finished:
		case <-time.After(time.Second):
			_ = command.Process.Kill()
			<-finished
		}
		require.NoError(t, os.RemoveAll(dir)) // Exact, generated, test-owned directory.
	})
	require.Eventually(
		t,
		func() bool { return client.Ping(context.Background()).Err() == nil },
		3*time.Second,
		20*time.Millisecond,
	)
	info, err := client.Info(context.Background(), "server").Result()
	require.NoError(t, err)
	require.Contains(t, info, "redis_version:")
	t.Log("backend: independently owned real Redis process and real Asynq consumer")
	guard, err := feishusafety.New(
		client,
		feishusafety.Config{
			Namespace:          "native-driver-" + uuid.NewString(),
			EndpointHost:       "gateway.invalid",
			InFlight:           1,
			RPM:                30,
			RPS:                1,
			TPM:                30000,
			WaitBudgetDuration: time.Second,
			TokenBoundVerified: true,
		},
	)
	require.NoError(t, err)
	host, err := os.Hostname()
	require.NoError(t, err)
	controller, err := New(
		client,
		guard,
		Config{
			Automatic:         true,
			AutomaticService:  true,
			DedicatedSyncLane: true,
			Scope:             "all",
			WorkerID:          fmt.Sprintf("%s:%d", host, os.Getpid()),
			SourceSHA:         strings.Repeat("a", 40),
			Maintenance:       4,
		},
		func(context.Context) error { return nil },
		func(Event) error { return nil },
	)
	require.NoError(t, err)
	controller.ApprovedConsumers = ApprovedConsumers(client)
	return &automaticService{
			c: controller,
		}, client, asynq.RedisClientOpt{
			Network: "unix",
			Addr:    socket,
		}
}

func startNativeAutomaticConsumer(t *testing.T, redisOpt asynq.RedisClientOpt) *asynq.Server {
	t.Helper()
	server := asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency:     1,
			Queues:          map[string]int{"sync": 1, ApprovedSyncQueue: 1},
			ShutdownTimeout: time.Second,
		},
	)
	require.NoError(
		t,
		server.Start(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })),
	)
	t.Cleanup(server.Shutdown)
	return server
}

func nativeAutomaticProof(t *testing.T, s *automaticService) systemHealthProof {
	t.Helper()
	var proof systemHealthProof
	require.Eventually(t, func() bool {
		c := s.c
		if register.Run(context.Background(), c.Client, []string{
			c.prefix + "workers",
			c.prefix + "leases",
			c.prefix + "joining",
			c.prefix + "admitted",
			c.prefix + "membership_revision",
		}, c.Config.WorkerID, c.fingerprint+":"+c.incarnation).
			Err() !=
			nil {
			return false
		}
		var err error
		proof, err = s.health(context.Background())
		return err == nil
	}, 4*time.Second, 30*time.Millisecond)
	return proof
}

func nativeAutomaticStable(t *testing.T, s *automaticService) systemHealthProof {
	t.Helper()
	start := time.Now()
	var proof systemHealthProof
	for {
		proof = nativeAutomaticProof(t, s)
		if s.stable.observe(time.Now(), proof, nil) {
			break
		}
		require.Less(
			t,
			time.Since(start),
			5*time.Second,
			"real healthy observation did not stabilize",
		)
		time.Sleep(150 * time.Millisecond)
	}
	require.GreaterOrEqual(
		t,
		s.stable.last.Sub(s.stable.started),
		3*time.Second,
		"only actual elapsed observation time establishes stability",
	)
	return proof
}

func assertNativeAutomaticClosed(t *testing.T, s *automaticService) {
	t.Helper()
	require.Zero(t, s.c.Client.Exists(context.Background(), s.c.Guard.PermitKey()).Val())
	require.Error(t, s.c.Guard.Check(context.Background()))
}

func TestAutomaticServiceNativeHealthyMetadataGapStableWindow(t *testing.T) {
	s, r, opt := nativeAutomaticService(t)
	startNativeAutomaticConsumer(t, opt)
	proof := nativeAutomaticStable(t, s)
	require.Len(t, proof.ConsumerServerIDs, 1)
	require.NotEmpty(t, proof.MembershipRevision)
	key := "asynq:servers:{" + proof.WorkerID + ":" + proof.ConsumerServerIDs[0] + "}"
	ctx := context.Background()
	raw, err := r.Get(ctx, key).Result()
	require.NoError(t, err)
	ttl, err := r.PTTL(ctx, key).Result()
	require.NoError(t, err)
	require.Positive(t, ttl)
	trip := "automatic_asynq_consumer_consumer_absent"
	require.NoError(t, r.Set(ctx, s.c.prefix+"trip", trip, 0).Err())
	require.NoError(t, r.Del(ctx, key).Err()) // Only this owned consumer's actual metadata.
	require.ErrorIs(t, s.step(ctx), ErrNotReady)
	require.Zero(t, s.stable.samples)
	journal, err := r.XRange(ctx, s.c.prefix+"system_journal", "-", "+").Result()
	require.NoError(t, err)
	require.NotEmpty(t, journal)
	diagnostic, ok := journal[len(journal)-1].Values["consumer_diagnostic"].(string)
	require.True(t, ok)
	require.Contains(t, diagnostic, "metadata_get_missing")
	require.NotContains(t, diagnostic, raw, "audit must never contain the captured raw protobuf")
	assertNativeAutomaticClosed(t, s)
	// Restore exactly the captured real heartbeat, not forged consumer metadata.
	// The real consumer continues its own heartbeat while recovery is observed.
	require.NoError(t, r.Set(ctx, key, raw, ttl).Err())
	recovered := nativeAutomaticProof(t, s)
	require.Equal(t, proof.ConsumerServerIDs, recovered.ConsumerServerIDs)
	require.False(
		t,
		s.stable.observe(time.Now(), recovered, nil),
		"one recovered read is not a stable window",
	)
	require.ErrorIs(
		t,
		s.step(ctx),
		ErrNotReady,
		"driver must wait, not enter SQL migration on first recovered sample",
	)
	assertNativeAutomaticClosed(t, s)
	nativeAutomaticStable(t, s)
	require.Equal(
		t,
		trip,
		r.Get(ctx, s.c.prefix+"trip").Val(),
		"health samples never erase historical evidence",
	)
	assertNativeAutomaticClosed(t, s) // No SQL receipt/canary/publication was issued.
}

func TestAutomaticServiceNativeConsumerExitAndReplacementIdentity(t *testing.T) {
	s, r, opt := nativeAutomaticService(t)
	first := startNativeAutomaticConsumer(t, opt)
	original := nativeAutomaticStable(t, s)
	incarnation := s.c.incarnation
	first.Shutdown()
	_, err := s.health(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, s.step(context.Background()), ErrNotReady)
	assertNativeAutomaticClosed(t, s)
	startNativeAutomaticConsumer(t, opt)
	replacement := nativeAutomaticProof(t, s)
	require.Equal(t, original.WorkerID, replacement.WorkerID)
	require.Equal(t, incarnation, s.c.incarnation)
	require.NotEqual(
		t,
		original.ConsumerServerIDs,
		replacement.ConsumerServerIDs,
		"real Asynq restart has a new server UUID",
	)
	require.False(
		t,
		s.stable.observe(time.Now(), replacement, nil),
		"replacement SID cannot inherit the old stable window",
	)
	require.ErrorIs(t, s.step(context.Background()), ErrNotReady)
	assertNativeAutomaticClosed(t, s)
	require.Zero(
		t,
		r.Exists(context.Background(), s.c.prefix+"system_service").Val(),
		"health-only fixture does not invent a durable SQL service epoch",
	)
}

func TestAutomaticServiceNativeExpiredMetadataAndCanceledProbe(t *testing.T) {
	s, r, opt := nativeAutomaticService(t)
	startNativeAutomaticConsumer(t, opt)
	proof := nativeAutomaticProof(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.health(ctx)
	require.Error(t, err)
	require.ErrorIs(t, s.step(ctx), ErrNotReady)
	assertNativeAutomaticClosed(t, s)
	key := "asynq:servers:{" + proof.WorkerID + ":" + proof.ConsumerServerIDs[0] + "}"
	require.NoError(t, r.PExpire(context.Background(), key, time.Millisecond).Err())
	time.Sleep(10 * time.Millisecond)
	_, err = s.health(context.Background())
	require.Error(t, err, "expired real heartbeat cannot attest liveness")
	require.ErrorIs(t, s.step(context.Background()), ErrNotReady)
	assertNativeAutomaticClosed(t, s)
}
