package feishusynccontrol

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// redisPauseHook pauses the disposable CI Redis once immediately before a
// selected command. This exercises real socket read deadlines, not a sleep in
// the controller's own Read implementation. Never enabled for a test-site DB.
type redisPauseHook struct {
	client  *redis.Client
	command string
	once    atomic.Bool
	paused  atomic.Bool
}

func (h *redisPauseHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *redisPauseHook) ProcessPipelineHook(
	next redis.ProcessPipelineHook,
) redis.ProcessPipelineHook {
	return next
}

func (h *redisPauseHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if h.command != "" && cmd.Name() == h.command && h.once.CompareAndSwap(false, true) {
			pause, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := h.client.Do(pause, "CLIENT", "PAUSE", 650, "ALL").Err(); err != nil {
				return err
			}
			h.paused.Store(true)
		}
		return next(ctx, cmd)
	}
}

func TestAutomaticRealConsumerChild(_ *testing.T) {
	if os.Getenv("FEISHU_CONTROL_CHILD") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("ready")
	if !scanner.Scan() || scanner.Text() != "start" {
		os.Exit(2)
	}
	server := asynq.NewServer(
		asynq.RedisClientOpt{Addr: os.Getenv("FEISHU_P0_REDIS_ADDR"), DB: 13},
		asynq.Config{
			Concurrency:     1,
			Queues:          map[string]int{"sync": 2, "low": 1},
			ShutdownTimeout: time.Second,
		},
	)
	if server.Start(
		asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil }),
	) != nil {
		os.Exit(3)
	}
	fmt.Println("started")
	if scanner.Scan() && scanner.Text() == "stop" {
		server.Shutdown()
		fmt.Println("stopped")
		scanner.Scan()
	}
	os.Exit(0)
}

// Uses two separate OS processes and real Redis/Asynq heartbeats. DB13 must be
// empty in this disposable CI service; no command addresses the test site's DB.
func TestAutomaticRealRedisTwoProcessJoiningAndLoss(t *testing.T) {
	runAutomaticRealRedisTwoProcess(t, "consumer_loss")
}

func TestAutomaticRealRedisTwoProcessBudgetFaults(t *testing.T) {
	for _, stage := range []string{"registry", "audit"} {
		t.Run(stage, func(t *testing.T) { runAutomaticRealRedisTwoProcess(t, stage) })
	}
}

func runAutomaticRealRedisTwoProcess(t *testing.T, faultStage string) {
	if os.Getenv("FEISHU_CONTROL_REAL_REDIS") != "1" {
		t.Skip("explicit disposable Redis gate required")
	}
	if faultStage != "consumer_loss" && os.Getenv("GITHUB_ACTIONS") != "true" &&
		os.Getenv("FEISHU_CONTROL_DISPOSABLE_REDIS") != "1" {
		t.Fatal("server pause requires an explicitly disposable loopback Redis service")
	}
	address := os.Getenv("FEISHU_P0_REDIS_ADDR")
	if address != "127.0.0.1:6379" {
		t.Fatal("only disposable loopback CI Redis is allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address, DB: 13})
	t.Cleanup(func() { _ = client.Close() })
	if n, err := client.DBSize(ctx).Result(); err != nil || n != 0 {
		t.Fatal("disposable Redis DB13 must be empty")
	}
	t.Cleanup(func() {
		// This database was empty before these child processes started. They are
		// reaped first; never FLUSHDB or touch another database/namespace.
		keys, err := client.Keys(context.Background(), "*").Result()
		if err == nil && len(keys) > 0 {
			_ = client.Del(context.Background(), keys...).Err()
		}
	})
	guard, err := feishusafety.New(client, feishusafety.Config{
		Namespace:    "real-join-" + uuid.NewString(),
		EndpointHost: "gateway.invalid", InFlight: 1, RPM: 5, RPS: 1, TPM: 1000,
		WaitBudgetDuration: time.Second, TokenBoundVerified: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	type child struct {
		command *exec.Cmd
		input   io.WriteCloser
		output  *bufio.Reader
	}
	spawn := func() *child {
		t.Helper()
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.CommandContext(
			ctx,
			executable,
			"-test.run=^TestAutomaticRealConsumerChild$",
		)
		command.Env = append(os.Environ(), "FEISHU_CONTROL_CHILD=1")
		input, err := command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := command.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		result := &child{command, input, bufio.NewReader(output)}
		t.Cleanup(func() { _ = input.Close(); _ = command.Process.Kill(); _ = command.Wait() })
		line, err := result.output.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "ready" {
			t.Fatal("child did not initialize")
		}
		return result
	}
	aProcess, bProcess := spawn(), spawn()
	hooks := map[string]*redisPauseHook{}
	controller := func(process *child) *Controller {
		t.Helper()
		host, err := os.Hostname()
		if err != nil {
			t.Fatal(err)
		}
		cfg := Config{
			Automatic: true, WorkerID: fmt.Sprintf("%s:%d", host, process.command.Process.Pid),
			SourceSHA: strings.Repeat("a", 40), Scope: "all", Maintenance: 1,
		}
		inspector := asynq.NewInspectorFromRedisClient(client)
		control, transport := NewControlClient(client)
		t.Cleanup(func() { _ = control.Close() })
		hook := &redisPauseHook{client: client}
		control.AddHook(hook)
		hooks[cfg.WorkerID] = hook
		c, err := New(
			control,
			guard,
			cfg,
			func(context.Context) error { return inspector.PauseQueue("sync") },
			func(e Event) error {
				return control.XAdd(e.Context(), &redis.XAddArgs{
					Stream: "control-test-audit", Values: map[string]any{"event": e.Kind},
				}).Err()
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		c.Transport = transport
		c.Consumers = SyncConsumers(control)
		return c
	}
	a, b := controller(aProcess), controller(bProcess)
	send := func(process *child, command, want string) {
		t.Helper()
		if _, err := fmt.Fprintln(process.input, command); err != nil {
			t.Fatal(err)
		}
		line, err := process.output.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != want {
			t.Fatalf("child %s did not finish", command)
		}
	}
	send(aProcess, "start", "started")
	deadline := time.Now().Add(5 * time.Second)
	for a.Step(ctx) != nil {
		if time.Now().After(deadline) {
			t.Fatal("first actual consumer not admitted")
		}
		time.Sleep(25 * time.Millisecond)
	}
	_ = b.Step(ctx) // controller exists before its actual consumer starts.
	if a.Step(ctx) == nil || client.Exists(ctx, a.prefix+"trip").Val() != 0 {
		t.Fatal("late real consumer falsely tripped the admitted controller")
	}
	if guard.Check(ctx) == nil {
		t.Fatal("joining consumer did not withdraw outbound permit")
	}
	send(bProcess, "start", "started")
	deadline = time.Now().Add(5 * time.Second)
	for {
		aErr, bErr := a.Step(ctx), b.Step(ctx)
		if aErr == nil && bErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real two-process topology never admitted")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if guard.Check(ctx) != nil {
		t.Fatal("complete topology remained closed")
	}
	if faultStage != "consumer_loss" {
		hook := hooks[a.Config.WorkerID]
		// The actual CI Redis pauses 650ms; use the production 500ms Step
		// deadline. It expires before Redis resumes to acknowledge the audit.
		hook.command = "evalsha"
		wantStage, wantTrip := "registry", "registry_network_timeout"
		if faultStage == "audit" {
			hook.command = "xadd"
			a.lastAlertAck = time.Time{}
			wantStage, wantTrip = "audit", "audit_unavailable"
		}
		var evidence StepTiming
		a.Observe = func(v StepTiming) { evidence = v }
		bounded, cancelStep := context.WithTimeout(ctx, 500*time.Millisecond)
		err := a.Step(bounded)
		cancelStep()
		if err == nil || guard.Check(ctx) == nil ||
			client.Exists(ctx, "asynq:{sync}:paused").Val() != 1 {
			t.Fatal("injected response delay did not stop both real processes")
		}
		trip := client.Get(ctx, a.prefix+"trip").Val()
		if faultStage == "audit" && trip != wantTrip {
			t.Fatal("audit deadline was misclassified", trip)
		}
		if len(evidence.Stages) == 0 || evidence.Stages[0].Stage != "registry" ||
			evidence.ID == "" {
			t.Fatal("missing exact-instance timing evidence")
		}
		found := false
		for _, stage := range evidence.Stages {
			if stage.Stage == wantStage {
				found = true
			}
		}
		if !found {
			t.Fatal("missing delayed phase evidence")
		}
		if !hook.paused.Load() {
			t.Fatal("Redis did not acknowledge actual server pause injection")
		}
		for _, stage := range evidence.Stages {
			if stage.Stage == wantStage {
				if stage.Finished.Sub(stage.Started) < 400*time.Millisecond {
					t.Fatal("injected server pause did not exercise the production control budget")
				}
				t.Logf(
					"CONTROL_BUDGET_FAULT stage=%s budget_ms=500 elapsed_ms=%d remaining_at_start_ms=%d "+
						"commands=%d io_spans=%d outbound_closed=true trip_retained=true",
					wantStage,
					stage.Finished.Sub(stage.Started).Milliseconds(),
					stage.RemainingMS,
					len(evidence.Commands),
					len(evidence.IO),
				)
			}
		}
		hook.command = ""
		if b.Step(ctx) == nil || guard.Check(ctx) == nil ||
			client.Exists(ctx, a.Guard.PermitKey()).Val() != 0 {
			t.Fatal("healthy peer or delayed acknowledgement reopened the stopped gate")
		}
		return
	}
	send(bProcess, "stop", "stopped") // controller still heartbeats: isolate consumer-only loss.
	deadline = time.Now().Add(12 * time.Second)
	for client.Exists(ctx, a.prefix+"trip").Val() == 0 {
		_ = a.Step(ctx)
		_ = b.Step(ctx)
		if time.Now().After(deadline) {
			t.Fatal("lost admitted Asynq consumer was not detected")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if guard.Check(ctx) == nil || client.Exists(ctx, "asynq:{sync}:paused").Val() != 1 {
		t.Fatal("actual consumer loss failed to fence outbound and pause sync")
	}
	if a.Step(ctx) == nil {
		t.Fatal("consumer loss silently recovered")
	}
}
