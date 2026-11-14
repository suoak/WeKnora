package feishusynccontrol

import (
	"context"
	"errors"
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

// ControlClient reserves a separate bounded pool for controller cycles. Source
// admission and publication checks retain their own pool and caller deadlines.
// Both pools use the same Redis endpoint, DB and safety namespace.
type ControlClient struct {
	*redis.Client
	probe *redis.Client
}

// Close releases both the source-operation and reserved controller pools.
func (c *ControlClient) Close() error {
	return errors.Join(c.Client.Close(), c.probe.Close())
}

// AddHook installs the same observer on both bounded pools.
func (c *ControlClient) AddHook(hook redis.Hook) {
	c.Client.AddHook(hook)
	c.probe.AddHook(hook)
}

type controllerProbeKey struct{}

type probeRouteHook struct{ probe *redis.Client }

func (h *probeRouteHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *probeRouteHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		// Connection initialization uses a sticky connection and inherits the
		// caller's context. Routing its handshake to the probe pool leaves the
		// original physical connection unauthenticated or on the wrong DB.
		if connectionInitializationCommand(cmd.Name()) {
			return next(ctx, cmd)
		}
		if ctx.Value(controllerProbeKey{}) == true {
			// Controller cycles use standalone commands and Lua. WATCH/MULTI
			// state cannot move safely between pooled connections.
			switch cmd.Name() {
			case "watch", "unwatch", "multi", "exec":
				return ErrNotReady
			}
			return h.probe.Process(ctx, cmd)
		}
		return next(ctx, cmd)
	}
}

func (h *probeRouteHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			if connectionInitializationCommand(cmd.Name()) {
				return next(ctx, cmds)
			}
		}
		if ctx.Value(controllerProbeKey{}) != true {
			return next(ctx, cmds)
		}
		if len(cmds) > 0 && cmds[0].Name() == "multi" {
			return ErrNotReady
		}
		_, err := h.probe.Pipelined(ctx, func(pipe redis.Pipeliner) error {
			for _, cmd := range cmds {
				if err := pipe.Process(ctx, cmd); err != nil {
					return err
				}
			}
			return nil
		})
		return err
	}
}

func connectionInitializationCommand(name string) bool {
	switch name {
	case "hello", "auth", "select", "client", "readonly":
		return true
	default:
		return false
	}
}

// NewControlClient preserves endpoint, credentials, TLS and DB. Deadlines and
// retries are deliberately stricter than the business pool, never more lenient.
func NewControlClient(base *redis.Client) (*ControlClient, *ControlTransport) {
	opts := *base.Options()
	opts.PoolSize, opts.MinIdleConns, opts.MaxIdleConns = 2, 0, 2
	opts.MaxActiveConns = 2
	opts.ContextTimeoutEnabled = true
	opts.DialTimeout = 500 * time.Millisecond
	opts.ReadTimeout = 500 * time.Millisecond
	opts.WriteTimeout = 500 * time.Millisecond
	opts.PoolTimeout = 500 * time.Millisecond
	opts.MaxRetries = -1
	// The initialized default dialer closes over the original Options. Rebuild
	// it with the bounded options, preserving TLS settings.
	opts.Dialer = redis.NewDialer(&opts)
	probeOpts := opts
	client := redis.NewClient(&opts)
	probe := redis.NewClient(&probeOpts)
	transport := &ControlTransport{}
	client.AddHook(&probeRouteHook{probe: probe})
	client.AddHook(&controlHook{transport: transport, client: client})
	probe.AddHook(&controlHook{transport: transport, client: probe})
	return &ControlClient{Client: client, probe: probe}, transport
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

// snapshot preserves the failed probe before independent stop commands start.
// It neither resets the active deadline nor consumes the final log sample.
func (t *ControlTransport) snapshot() ([]CommandTiming, []IOTiming) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]CommandTiming(nil), t.commands...), append([]IOTiming(nil), t.io...)
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
	// Only bare Background/TODO calls from legacy clients may borrow Step.
	// Requests and scoped tasks retain their own cancellation even when they
	// have no deadline; a concurrent Step must never authorize or cancel them.
	if _, ok := ctx.Deadline(); ok || (ctx != context.Background() && ctx != context.TODO()) {
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
	bounded := h.bound(ctx)
	err := run(bounded)
	// A successful response arriving after the caller's budget is not a valid
	// control acknowledgement. This also fences post-read scheduling delays.
	if err == nil {
		err = bounded.Err()
	}
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
