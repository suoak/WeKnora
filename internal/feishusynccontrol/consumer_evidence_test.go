package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

func evidenceFixture(t *testing.T) (*Controller, *redis.Client, *asynq.Server, string, string) {
	t.Helper()
	f := newRecoveryFixture(t)
	host, err := os.Hostname()
	require.NoError(t, err)
	g, err := feishusafety.New(
		f.r,
		feishusafety.Config{
			Namespace:          "evidence-" + uuid.NewString(),
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
	c, err := New(
		f.r,
		g,
		Config{
			Automatic:         true,
			DedicatedSyncLane: true,
			WorkerID:          fmt.Sprintf("%s:%d", host, os.Getpid()),
			SourceSHA:         strings.Repeat("a", 40),
			Scope:             "all",
			Maintenance:       4,
		},
		func(context.Context) error { return nil },
		func(Event) error { return nil },
	)
	require.NoError(t, err)
	server := asynq.NewServer(
		asynq.RedisClientOpt{Addr: f.r.Options().Addr},
		asynq.Config{
			Concurrency:     1,
			Queues:          map[string]int{"sync": 1, ApprovedSyncQueue: 1},
			ShutdownTimeout: time.Second,
		},
	)
	require.NoError(t, server.Start(asynq.NewServeMux()))
	t.Cleanup(server.Shutdown)
	ctx := context.Background()
	require.Eventually(
		t,
		func() bool { return f.r.ZCard(ctx, "asynq:servers").Val() == 1 },
		time.Second,
		time.Millisecond,
	)
	key := f.r.ZRange(ctx, "asynq:servers", 0, -1).Val()[0]
	var raw string
	require.Eventually(
		t,
		func() bool { raw = f.r.Get(ctx, key).Val(); return raw != "" },
		time.Second,
		time.Millisecond,
	)
	c.Consumers = ApprovedConsumers(f.r)
	require.NoError(t, c.Step(ctx))
	return c, f.r, server, key, raw
}

func retainedConsumerEvidence(t *testing.T, c *Controller, r *redis.Client) consumerEvidence {
	t.Helper()
	rows, err := r.XRange(context.Background(), c.prefix+"control_audit", "-", "+").Result()
	require.NoError(t, err)
	for _, row := range rows {
		if s, ok := row.Values["consumer_diagnostic"].(string); ok && s != "" {
			var d consumerEvidence
			require.NoError(t, json.Unmarshal([]byte(s), &d))
			return d
		}
	}
	t.Fatal("first failure evidence missing")
	return consumerEvidence{}
}

func TestConsumerEvidenceTransientMissingMetadataRemainsLatched(t *testing.T) {
	c, r, _, key, raw := evidenceFixture(t)
	ctx := context.Background()
	require.NoError(t, r.Del(ctx, key).Err())
	require.Error(t, c.Step(ctx))
	d := retainedConsumerEvidence(t, c, r)
	require.Equal(t, "metadata_get_missing", d.Servers[0].MetadataClass)
	require.EqualValues(t, -2, d.Servers[0].TTLMS)
	require.True(t, d.IncarnationMatches)
	start := time.Now()
	time.Sleep(600 * time.Millisecond)
	require.NoError(t, r.Set(ctx, key, raw, 10*time.Second).Err())
	_, err := ApprovedConsumers(r)(ctx)
	require.NoError(t, err)
	t.Logf(
		"consumer visible after %.3fs; synthetic missing-key window, not proof of production cause",
		time.Since(start).Seconds(),
	)
	require.Error(t, c.Step(ctx))
	require.True(t, r.Exists(ctx, c.prefix+"trip").Val() == 1)
	require.EqualValues(t, 0, r.Exists(ctx, c.Guard.PermitKey()).Val())
	rows := r.XRange(ctx, c.prefix+"control_audit", "-", "+").Val()
	count := 0
	for _, row := range rows {
		if row.Values["consumer_diagnostic"] != "" {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestConsumerEvidenceCorruptMetadataAndNoSensitivePayload(t *testing.T) {
	c, r, _, key, _ := evidenceFixture(t)
	ctx := context.Background()
	require.NoError(t, r.Set(ctx, key, "\xffsecret-token SQL body", 10*time.Second).Err())
	require.Error(t, c.Step(ctx))
	d := retainedConsumerEvidence(t, c, r)
	require.Equal(t, "metadata_decode_failed", d.Servers[0].MetadataClass)
	b, err := json.Marshal(d)
	require.NoError(t, err)
	require.NotContains(t, string(b), "secret-token")
	require.NotContains(t, string(b), "SQL body")
}

func TestConsumerEvidenceHeartbeatAndRealConsumerExit(t *testing.T) {
	c, r, server, _, _ := evidenceFixture(t)
	ctx := context.Background()
	// Cross the real Asynq heartbeat update boundary while repeatedly reading.
	until := time.Now().Add(6 * time.Second)
	for time.Now().Before(until) {
		require.NoError(t, c.Step(ctx))
		time.Sleep(40 * time.Millisecond)
	}
	server.Shutdown()
	require.Error(t, c.Step(ctx))
	require.EqualValues(t, 1, r.Exists(ctx, c.prefix+"trip").Val())
	require.EqualValues(t, 0, r.Exists(ctx, c.Guard.PermitKey()).Val())
	d := retainedConsumerEvidence(t, c, r)
	require.Equal(t, "registry_members_absent", d.RegistryClass)
	require.NotEmpty(t, d.MissingPreviousServerIDs)
}

func TestConsumerEvidenceUnsubscribedQueueAndMetadataIdentity(t *testing.T) {
	for _, mode := range []string{"queue", "approved_queue", "identity"} {
		t.Run(mode, func(t *testing.T) {
			c, r, _, key, raw := evidenceFixture(t)
			ctx := context.Background()
			desc, err := protoregistry.GlobalFiles.FindDescriptorByName("asynq.ServerInfo")
			require.NoError(t, err)
			msg := dynamicpb.NewMessage(desc.(protoreflect.MessageDescriptor))
			require.NoError(t, proto.Unmarshal([]byte(raw), msg))
			switch mode {
			case "queue":
				msg.Clear(msg.Descriptor().Fields().ByName("queues"))
			case "approved_queue":
				queues := msg.Mutable(msg.Descriptor().Fields().ByName("queues")).Map()
				queues.Clear(protoreflect.ValueOfString(ApprovedSyncQueue).MapKey())
			default:
				msg.Set(msg.Descriptor().Fields().ByName("host"), protoreflect.ValueOfString("different-host"))
			}
			data, err := proto.Marshal(msg)
			require.NoError(t, err)
			require.NoError(t, r.Set(ctx, key, data, 10*time.Second).Err())
			require.Error(t, c.Step(ctx))
			d := retainedConsumerEvidence(t, c, r)
			if mode == "queue" || mode == "approved_queue" {
				require.Equal(t, "expected_queue_not_subscribed", d.Servers[0].MetadataClass)
			} else {
				require.Equal(t, "metadata_identity_mismatch", d.Servers[0].MetadataClass)
			}
			if mode == "approved_queue" {
				require.Equal(t, "expected_queue_not_subscribed", d.ProbeClass)
			}
			require.EqualValues(t, 0, r.Exists(ctx, c.Guard.PermitKey()).Val())
		})
	}
}

func TestConsumerEvidenceTTLBoundaryIsNotProofOfDeletionCause(t *testing.T) {
	c, r, _, key, _ := evidenceFixture(t)
	ctx := context.Background()
	require.NoError(t, r.PExpire(ctx, key, 5*time.Millisecond).Err())
	time.Sleep(15 * time.Millisecond)
	// In miniredis, time passage is explicit; the same test also runs against
	// real Redis, where the short TTL above expires normally.
	if os.Getenv("FEISHU_RECOVERY_REAL_REDIS_ADDR") == "" {
		require.NoError(t, r.Del(ctx, key).Err())
	}
	require.Error(t, c.Step(ctx))
	d := retainedConsumerEvidence(t, c, r)
	require.Equal(t, "metadata_get_missing", d.Servers[0].MetadataClass)
	require.Equal(t, "key_missing_expiry_or_deletion_unknown", d.Servers[0].ExpiryEvidence)
}

type revisionDuringProbe struct {
	redis.UniversalClient
	prefix string
}

func (r revisionDuringProbe) Ping(ctx context.Context) *redis.StatusCmd {
	_ = r.UniversalClient.Incr(ctx, r.prefix+"membership_revision").Err()
	return r.UniversalClient.Ping(ctx)
}

func TestConsumerEvidenceMembershipRevisionRaceStaysClosed(t *testing.T) {
	c, r, _, _, _ := evidenceFixture(t)
	ctx := context.Background()
	c.Consumers = ApprovedConsumers(revisionDuringProbe{r, c.prefix})
	require.Error(t, c.Step(ctx))
	require.EqualValues(t, 0, r.Exists(ctx, c.Guard.PermitKey()).Val())
	require.EqualValues(
		t,
		0,
		r.Exists(ctx, c.prefix+"trip").Val(),
		"existing REGISTRY_CHANGED join behavior must remain unchanged",
	)
}

func TestConsumerEvidenceBoundsReadOnlySnapshotAndExpiry(t *testing.T) {
	c, r, _, key, _ := evidenceFixture(t)
	ctx := context.Background()
	d := newConsumerEvidence(ctx, r, true)
	d.read(key, "", redis.Nil)
	require.NoError(t, r.ZAdd(ctx, "asynq:servers", redis.Z{Score: float64(time.Now().Unix() - 10), Member: key}).Err())
	require.NoError(t, r.Del(ctx, key).Err())
	before := r.ZRangeWithScores(ctx, "asynq:servers", 0, -1).Val()
	text := d.snapshot(ctx, c, "synthetic_expiry")
	require.Contains(t, text, "metadata_missing_expired_registry_score")
	require.Equal(
		t,
		before,
		r.ZRangeWithScores(ctx, "asynq:servers", 0, -1).Val(),
		"diagnostic must not clean expired registry entries",
	)
	for i := 0; i < 100; i++ {
		d.read("asynq:servers:{host:1:"+uuid.NewString()+"}", "sensitive", nil)
	}
	require.LessOrEqual(t, len(d.Servers), consumerEvidenceLimit)
	require.True(t, d.Truncated)
}

func TestConsumerEvidenceCanceledAndSlowRedis(t *testing.T) {
	c, r, _, _, _ := evidenceFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var d *consumerEvidence
	_, err := ApprovedConsumers(r)(context.WithValue(ctx, consumerEvidenceKey{}, &d))
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "context_canceled", d.ProbeClass)
	// Hook injects a bounded transport stall, not a longer probe timeout.
	r.AddHook(consumerReadFailureHook{cause: context.DeadlineExceeded})
	start := time.Now()
	require.Error(t, c.Step(context.Background()))
	require.Less(t, time.Since(start), time.Second)
	diag := retainedConsumerEvidence(t, c, r)
	require.Equal(t, "context_deadline", diag.ProbeClass)
}

func TestConsumerEvidenceIncarnationAndRevision(t *testing.T) {
	c, r, _, _, _ := evidenceFixture(t)
	ctx := context.Background()
	other := uuid.NewString()
	require.NoError(t, r.HSet(ctx, c.prefix+"admitted", c.Config.WorkerID, c.fingerprint+":"+other).Err())
	require.NoError(t, r.Incr(ctx, c.prefix+"membership_revision").Err())
	require.Error(t, c.Step(ctx))
	d := retainedConsumerEvidence(t, c, r)
	require.False(t, d.IncarnationMatches)
	require.Equal(t, other, d.AdmittedIncarnation)
	require.NotEmpty(t, d.PostFailureMembershipRevision)
}

type delayedEvidenceConn struct{ net.Conn }

func (c delayedEvidenceConn) Read(b []byte) (int, error) {
	time.Sleep(80 * time.Millisecond)
	return c.Conn.Read(b)
}

func TestConsumerEvidenceRealRedisSlowResponse(t *testing.T) {
	c, r, _, _, _ := evidenceFixture(t)
	client := redis.NewClient(
		&redis.Options{
			Addr:                  r.Options().Addr,
			MaxRetries:            -1,
			ReadTimeout:           20 * time.Millisecond,
			ContextTimeoutEnabled: true,
			Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				return delayedEvidenceConn{conn}, nil
			},
		},
	)
	t.Cleanup(func() { _ = client.Close() })
	c.Consumers = ApprovedConsumers(client)
	start := time.Now()
	require.Error(t, c.Step(context.Background()))
	require.Less(t, time.Since(start), time.Second)
	d := retainedConsumerEvidence(t, c, r)
	require.Equal(t, "redis_timeout", d.ProbeClass)
	require.EqualValues(t, 0, r.Exists(context.Background(), c.Guard.PermitKey()).Val())
}

func TestConsumerEvidenceRetainsFirstFailureAcrossAuditFailure(t *testing.T) {
	c, r, _, key, raw := evidenceFixture(t)
	ctx := context.Background()
	var delivered []string
	failed := true
	c.Audit = func(event Event) error {
		if event.Kind == "feishu_safety_stop" {
			delivered = append(delivered, event.ConsumerDiagnostic)
			if failed {
				return errors.New("synthetic audit unavailable")
			}
		}
		return nil
	}
	require.NoError(t, r.Del(ctx, key).Err())
	require.Error(t, c.Step(ctx))
	first := c.consumerDiagnosticPending
	require.NotEmpty(t, first)
	require.Contains(t, first, "metadata_get_missing")
	require.False(t, c.consumerDiagnosticRecorded)
	require.NoError(t, r.Set(ctx, key, raw, 10*time.Second).Err())
	failed = false
	require.Error(t, c.Step(ctx))
	require.True(t, c.consumerDiagnosticRecorded)
	require.Empty(t, c.consumerDiagnosticPending)
	require.Equal(t, []string{first, first}, delivered)
	require.EqualValues(t, 1, r.Exists(ctx, c.prefix+"trip").Val())
	require.EqualValues(t, 0, r.Exists(ctx, c.Guard.PermitKey()).Val())
}

type evidenceAuditWriteFailure struct {
	consumerReadFailureHook
	fail   *atomic.Bool
	stream string
}

func (h evidenceAuditWriteFailure) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "xadd" && fmt.Sprint(cmd.Args()[1]) == h.stream && h.fail.Load() {
			return errors.New("synthetic durable audit write failure")
		}
		return next(ctx, cmd)
	}
}

func TestConsumerEvidenceRetainsFirstFailureAcrossDurableWriteFailure(t *testing.T) {
	c, r, _, key, raw := evidenceFixture(t)
	ctx := context.Background()
	var fail atomic.Bool
	fail.Store(true)
	r.AddHook(evidenceAuditWriteFailure{fail: &fail, stream: c.prefix + "control_audit"})
	require.NoError(t, r.Del(ctx, key).Err())
	require.Error(t, c.Step(ctx))
	first := c.consumerDiagnosticPending
	require.NotEmpty(t, first)
	require.EqualValues(t, 0, r.XLen(ctx, c.prefix+"control_audit").Val())
	require.NoError(t, r.Set(ctx, key, raw, 10*time.Second).Err())
	fail.Store(false)
	require.Error(t, c.Step(ctx))
	rows := r.XRange(ctx, c.prefix+"control_audit", "-", "+").Val()
	require.Len(t, rows, 1)
	require.Equal(t, first, rows[0].Values["consumer_diagnostic"])
	require.EqualValues(t, 1, r.Exists(ctx, c.prefix+"trip").Val())
	require.EqualValues(t, 0, r.Exists(ctx, c.Guard.PermitKey()).Val())
}
