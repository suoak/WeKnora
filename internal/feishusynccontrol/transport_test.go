package feishusynccontrol

import (
	"context"
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type delayedReadConn struct {
	net.Conn
	delay   *atomic.Bool
	latency *atomic.Int64
}

func (c *delayedReadConn) Read(b []byte) (int, error) {
	if c.delay.Swap(false) {
		delay := time.Duration(c.latency.Load())
		if delay == 0 {
			delay = 80 * time.Millisecond
		}
		time.Sleep(delay)
	}
	return c.Conn.Read(b)
}

type delayReplyHook struct {
	delay   atomic.Bool
	latency atomic.Int64
	command string
}

type latePermitAckHook struct{ enabled atomic.Bool }

func (h *latePermitAckHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *latePermitAckHook) ProcessPipelineHook(
	next redis.ProcessPipelineHook,
) redis.ProcessPipelineHook {
	return next
}

func (h *latePermitAckHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		args := cmd.Args()
		if h.enabled.Load() && err == nil && cmd.Name() == "evalsha" && len(args) > 1 &&
			args[1] == automaticPermit.Hash() {
			time.Sleep(80 * time.Millisecond)
		}
		return err
	}
}

func (h *delayReplyHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, n, a string) (net.Conn, error) {
		c, e := next(ctx, n, a)
		if e != nil {
			return nil, e
		}
		return &delayedReadConn{Conn: c, delay: &h.delay, latency: &h.latency}, nil
	}
}

func (h *delayReplyHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == h.command {
			h.delay.Store(true)
		}
		return next(ctx, cmd)
	}
}

func (h *delayReplyHook) ProcessPipelineHook(
	next redis.ProcessPipelineHook,
) redis.ProcessPipelineHook {
	return next
}

func TestControlTransportBoundsInspectorBackgroundAndMeasuresReply(t *testing.T) {
	f := newControlFixture(t)
	client, transport := NewControlClient(f.client)
	t.Cleanup(func() { _ = client.Close() })
	if client.Options().DB != f.client.Options().DB ||
		client.Options().Addr != f.client.Options().Addr ||
		!client.Options().ContextTimeoutEnabled ||
		client.Options().MaxRetries != 0 {
		t.Fatal("control transport changed endpoint or left unbounded retries/deadlines")
	}
	hook := &delayReplyHook{}
	client.AddHook(hook)
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	transport.begin(ctx)
	hook.command = "ping"
	err := client.Ping(context.Background()).Err() // matches Inspector's lost Context
	commands, io := transport.finish()
	if err == nil || len(commands) != 1 || !commands[0].Failed || len(io) == 0 {
		t.Fatal("background probe escaped the enclosing deadline or lost measurements")
	}
	if commands[0].Finished.Sub(commands[0].Started) < 70*time.Millisecond {
		t.Fatal("reply delay not observed")
	}
	// An expired Step must not replace the independent stop's explicit context.
	transport.begin(ctx)
	hook.command = ""
	stop, cancelStop := context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()
	if err = client.Ping(stop).Err(); err != nil {
		t.Fatal("Step cancellation canceled independent fencing", err)
	}
	transport.finish()
}

func TestControlTransportIndependentPoolAndAuditFailureStayClosed(t *testing.T) {
	f := newControlFixture(t)
	client, transport := NewControlClient(f.client)
	t.Cleanup(func() { _ = client.Close() })
	c := f.automaticController(t, "app:1")
	hook := &delayReplyHook{}
	client.AddHook(hook)
	c.Consumers = func(context.Context) ([]string, error) { return []string{"app:1"}, nil }
	c.Client, c.Transport = client, transport
	var timing StepTiming
	c.Observe = func(v StepTiming) { timing = v }
	c.Audit = func(event Event) error {
		return client.XAdd(event.Context(), &redis.XAddArgs{
			Stream: c.prefix + "test-events", Values: map[string]any{"kind": event.Kind},
		}).Err()
	}
	if err := c.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.lastAlertAck = time.Time{}
	hook.command = "xadd"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if c.Step(ctx) == nil || f.guard.Check(context.Background()) == nil {
		t.Fatal("late audit granted a permit")
	}
	if f.client.Get(context.Background(), c.prefix+"trip").Val() != "audit_unavailable" {
		t.Fatal("audit failure was not latched")
	}
	if len(timing.Stages) < 3 || timing.Stages[1].Stage != "audit" ||
		timing.Stages[2].Stage != "stop" {
		t.Fatal("audit and independent stop timing conflated")
	}
	if timing.ID == "" || timing.Incarnation == "" || !timing.Failed {
		t.Fatal("missing instance/correlation/error evidence")
	}
	records, err := client.XRevRangeN(context.Background(), c.prefix+"control_audit", "+", "-", 1).Result()
	if err != nil || len(records) != 1 {
		t.Fatal("failed probe evidence was not durably acknowledged", err)
	}
	var persisted StepTiming
	raw, ok := records[0].Values["probe_timing"].(string)
	if !ok || json.Unmarshal([]byte(raw), &persisted) != nil || persisted.ID != timing.ID ||
		len(persisted.Stages) != 2 || persisted.Stages[1].Stage != "audit" || !persisted.Failed {
		t.Fatal("retained audit lost the exact failed probe or included later stop work")
	}
	if len(persisted.Commands) == 0 || !persisted.Commands[len(persisted.Commands)-1].Failed ||
		persisted.Commands[len(persisted.Commands)-1].Operation != "xadd" {
		t.Fatal("retained audit lost the failed command")
	}
	for _, s := range timing.Stages {
		if s.Stage == "permit" {
			t.Fatal("audit failure reached permit phase")
		}
	}
}

func TestControlTransportBusinessPoolStarvationCannotDelayAudit(t *testing.T) {
	f := newControlFixture(t)
	base := redis.NewClient(
		&redis.Options{Addr: f.server.Addr(), PoolSize: 1, MaxActiveConns: 1, MaxRetries: -1},
	)
	t.Cleanup(func() { _ = base.Close() })
	control, transport := NewControlClient(base)
	t.Cleanup(func() { _ = control.Close() })
	if err := control.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- base.BLPop(context.Background(), time.Second, "held-business-connection").Err() }()
	deadline := time.Now().Add(time.Second)
	for base.PoolStats().TotalConns != 1 || base.PoolStats().IdleConns != 0 {
		if time.Now().After(deadline) {
			t.Fatal("business connection was not claimed")
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if base.XAdd(ctx, &redis.XAddArgs{Stream: "shared-audit", Values: map[string]any{"event": "test"}}).
		Err() ==
		nil {
		t.Fatal("fault injection did not exhaust the shared business pool")
	}
	bounded, cancelControl := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelControl()
	transport.begin(bounded)
	err := control.XAdd(bounded, &redis.XAddArgs{
		Stream: "isolated-control-audit", Values: map[string]any{"event": "test"},
	}).Err()
	commands, _ := transport.finish()
	if err != nil || len(commands) != 1 || commands[0].PoolWaitNS >= int64(20*time.Millisecond) {
		t.Fatalf(
			"business pool starvation leaked into control audit: failed=%t commands=%d",
			err != nil,
			len(commands),
		)
	}
	if err = control.LPush(context.Background(), "held-business-connection", "release").Err(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestControlTransportLatePermitAcknowledgementCannotOpenOutbound(t *testing.T) {
	f := newControlFixture(t)
	client, transport := NewControlClient(f.client)
	t.Cleanup(func() { _ = client.Close() })
	hook := &latePermitAckHook{}
	client.AddHook(hook)
	c := f.automaticController(t, "app:1")
	c.Client, c.Transport = client, transport
	c.Consumers = func(context.Context) ([]string, error) { return []string{"app:1"}, nil }
	if err := c.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	hook.enabled.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if c.Step(ctx) == nil || f.guard.Check(context.Background()) == nil {
		t.Fatal("a late successful permit acknowledgement allowed outbound work")
	}
	if f.client.Get(context.Background(), c.prefix+"trip").
		Val() !=
		"automatic_permit_context_deadline" ||
		f.client.Exists(context.Background(), c.Guard.PermitKey()).Val() != 0 {
		t.Fatal("late permit acknowledgement was not revoked and latched")
	}
}
