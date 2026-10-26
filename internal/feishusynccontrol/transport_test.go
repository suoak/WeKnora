package feishusynccontrol

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type delayedReadConn struct {
	net.Conn
	delay *atomic.Bool
}

func (c *delayedReadConn) Read(b []byte) (int, error) {
	if c.delay.Swap(false) {
		time.Sleep(80 * time.Millisecond)
	}
	return c.Conn.Read(b)
}

type delayReplyHook struct {
	delay   atomic.Bool
	command string
}

func (h *delayReplyHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, n, a string) (net.Conn, error) {
		c, e := next(ctx, n, a)
		if e != nil {
			return nil, e
		}
		return &delayedReadConn{Conn: c, delay: &h.delay}, nil
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
	defer client.Close()
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
	defer client.Close()
	c := f.automaticController(t, "app:1")
	hook := &delayReplyHook{}
	client.AddHook(hook)
	c.Consumers = func(context.Context) ([]string, error) { return []string{"app:1"}, nil }
	c.Client, c.Transport = client, transport
	var timing StepTiming
	c.Observe = func(v StepTiming) { timing = v }
	c.Audit = func(event Event) error {
		return client.XAdd(event.Context(), &redis.XAddArgs{Stream: c.prefix + "test-events", Values: map[string]any{"kind": event.Kind}}).
			Err()
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
	for _, s := range timing.Stages {
		if s.Stage == "permit" {
			t.Fatal("audit failure reached permit phase")
		}
	}
}
