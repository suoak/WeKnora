package feishusynccontrol

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// ControlTransport owns an isolated pool, not a new Redis namespace. Business
// traffic cannot consume its connections. Asynq's Inspector uses Background;
// the hook restores the enclosing Step deadline only for such unbounded calls.
type ControlTransport struct {
	mu       sync.Mutex
	step     context.Context
	commands []CommandTiming
	io       []IOTiming
}

// IOTiming records read/write boundaries. Read includes server response time and network time; it is not an fsync or
// server execution measurement. Pool wait comes from Redis's pool counters.
type IOTiming struct {
	Operation string    `json:"operation"`
	Started   time.Time `json:"started"`
	Finished  time.Time `json:"finished"`
	Bytes     int       `json:"bytes"`
	Failed    bool      `json:"failed"`
}

// CommandTiming records acknowledged commands without their arguments or errors.
type CommandTiming struct {
	Operation  string    `json:"operation"`
	Started    time.Time `json:"started"`
	Finished   time.Time `json:"finished"`
	PoolWaitNS int64     `json:"pool_wait_ns"`
	Failed     bool      `json:"failed"`
}

// NewControlClient preserves endpoint, credentials, TLS and DB. Deadlines and
// retries are deliberately stricter than the business pool, never more lenient.
func NewControlClient(base *redis.Client) (*redis.Client, *ControlTransport) {
	opts := *base.Options()
	opts.PoolSize, opts.MinIdleConns, opts.MaxIdleConns = 2, 0, 2
	opts.MaxActiveConns = 2
	opts.ContextTimeoutEnabled = true
	opts.DialTimeout, opts.ReadTimeout, opts.WriteTimeout, opts.PoolTimeout =
		500*time.Millisecond, 500*time.Millisecond, 500*time.Millisecond, 500*time.Millisecond
	opts.MaxRetries = -1
	// The initialized default dialer closes over the original Options. Rebuild
	// it with the bounded options, preserving TLS settings.
	opts.Dialer = redis.NewDialer(&opts)
	client := redis.NewClient(&opts)
	transport := &ControlTransport{}
	client.AddHook(&controlHook{transport: transport, client: client})
	return client, transport
}

func (t *ControlTransport) begin(ctx context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.step, t.commands, t.io = ctx, nil, nil
}

func (t *ControlTransport) finish() ([]CommandTiming, []IOTiming) {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := append([]CommandTiming(nil), t.commands...)
	io := append([]IOTiming(nil), t.io...)
	t.step, t.commands, t.io = nil, nil, nil
	return result, io
}

func (t *ControlTransport) recordIO(operation string, start time.Time, n int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.step != nil && len(t.io) < 128 {
		t.io = append(
			t.io,
			IOTiming{
				Operation: operation,
				Started:   start,
				Finished:  time.Now().UTC(),
				Bytes:     n,
				Failed:    err != nil,
			},
		)
	}
}

type timedConn struct {
	net.Conn
	transport *ControlTransport
}

func (c *timedConn) Read(p []byte) (int, error) {
	start := time.Now().UTC()
	n, err := c.Conn.Read(p)
	c.transport.recordIO("read", start, n, err)
	return n, err
}

func (c *timedConn) Write(p []byte) (int, error) {
	start := time.Now().UTC()
	n, err := c.Conn.Write(p)
	c.transport.recordIO("write", start, n, err)
	return n, err
}

type controlHook struct {
	transport *ControlTransport
	client    *redis.Client
}

func (h *controlHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		start := time.Now().UTC()
		conn, err := next(ctx, network, addr)
		h.transport.recordIO("dial", start, 0, err)
		if err != nil {
			return nil, err
		}
		return &timedConn{Conn: conn, transport: h.transport}, nil
	}
}

func (h *controlHook) bound(ctx context.Context) context.Context {
	if _, ok := ctx.Deadline(); ok {
		return ctx
	}
	h.transport.mu.Lock()
	defer h.transport.mu.Unlock()
	if h.transport.step != nil {
		return h.transport.step
	}
	return ctx
}

func (h *controlHook) record(
	ctx context.Context,
	operation string,
	run func(context.Context) error,
) error {
	start := time.Now().UTC()
	before := h.client.PoolStats().WaitDurationNs
	err := run(h.bound(ctx))
	timing := CommandTiming{
		Operation: operation, Started: start, Finished: time.Now().UTC(),
		PoolWaitNS: h.client.PoolStats().WaitDurationNs - before, Failed: err != nil,
	}
	h.transport.mu.Lock()
	defer h.transport.mu.Unlock()
	if h.transport.step != nil && len(h.transport.commands) < 64 {
		h.transport.commands = append(h.transport.commands, timing)
	}
	return err
}

func (h *controlHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		// Command name only; never log arguments, keys, Redis errors or payloads.
		return h.record(
			ctx,
			cmd.Name(),
			func(bounded context.Context) error { return next(bounded, cmd) },
		)
	}
}

func (h *controlHook) ProcessPipelineHook(
	next redis.ProcessPipelineHook,
) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		return h.record(
			ctx,
			"pipeline",
			func(bounded context.Context) error { return next(bounded, cmds) },
		)
	}
}
