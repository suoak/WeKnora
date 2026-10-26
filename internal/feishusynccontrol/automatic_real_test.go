package feishusynccontrol

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

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
	if server.Start(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })) != nil {
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
	if os.Getenv("FEISHU_CONTROL_REAL_REDIS") != "1" {
		t.Skip("explicit disposable Redis gate required")
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
		command := exec.CommandContext(ctx, executable, "-test.run=^TestAutomaticRealConsumerChild$")
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
		c, err := New(
			client,
			guard,
			cfg,
			func(context.Context) error { return inspector.PauseQueue("sync") },
			func(Event) error { return nil },
		)
		if err != nil {
			t.Fatal(err)
		}
		c.Consumers = SyncConsumers(client)
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
