package router

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestAsynqPoolKeepsConsumerUntilApplicationCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shutdown signal regression")
	}
	addr := os.Getenv("FEISHU_RECOVERY_REAL_REDIS_ADDR")
	if addr == "" {
		t.Skip("requires isolated real Redis")
	}
	require.Regexp(t, `^knowhub-recovery-redis-[a-f0-9]{12}:6379$`, addr)
	queue := "shutdown-owner-" + uuid.NewString()
	child := exec.Command(os.Args[0], "-test.run=^TestAsynqPoolSignalChild$", "-test.timeout=25s")
	child.Env = append(os.Environ(), "KNOWHUB_ASYNQ_SIGNAL_CHILD=1", "KNOWHUB_ASYNQ_SIGNAL_QUEUE="+queue)
	stdout, err := child.StdoutPipe()
	require.NoError(t, err)
	stdin, err := child.StdinPipe()
	require.NoError(t, err)
	child.Stderr = io.Discard
	require.NoError(t, child.Start())
	t.Cleanup(func() { _ = child.Process.Kill() })
	lines := make(chan string, 16)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	await := func(wanted string) {
		t.Helper()
		timer := time.NewTimer(12 * time.Second)
		defer timer.Stop()
		for {
			select {
			case line, ok := <-lines:
				require.True(t, ok, "child exited before %s", wanted)
				if line == wanted {
					return
				}
			case <-timer.C:
				t.Fatalf("application lifecycle marker missing: %s", wanted)
			}
		}
	}
	await("KH_READY")
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: addr})
	defer func() { require.NoError(t, client.Close()) }()
	_, err = client.Enqueue(asynq.NewTask("shutdown-proof", nil), asynq.Queue(queue), asynq.MaxRetry(0))
	require.NoError(t, err)
	await("KH_PROCESSED")
	require.NoError(t, child.Process.Signal(syscall.SIGTERM))
	await("KH_APP_DRAINING")
	// HTTP draining has begun, but ordered safety cleanup has not yet run.
	// An independent Asynq signal handler would stop consuming this next task.
	_, err = client.Enqueue(asynq.NewTask("shutdown-proof", nil), asynq.Queue(queue), asynq.MaxRetry(0))
	require.NoError(t, err)
	await("KH_PROCESSED")
	_, err = fmt.Fprintln(stdin, "cleanup")
	require.NoError(t, err)
	await("KH_CLEANED")
	require.NoError(t, child.Wait())
}

func TestAsynqPoolSignalChild(t *testing.T) {
	if os.Getenv("KNOWHUB_ASYNQ_SIGNAL_CHILD") != "1" {
		t.Skip("subprocess only")
	}
	addr := os.Getenv("FEISHU_RECOVERY_REAL_REDIS_ADDR")
	require.True(t, regexp.MustCompile(`^knowhub-recovery-redis-[a-f0-9]{12}:6379$`).MatchString(addr))
	queue := os.Getenv("KNOWHUB_ASYNQ_SIGNAL_QUEUE")
	require.Contains(t, queue, "shutdown-owner-")
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	defer signal.Stop(signals)
	srv := asynq.NewServer(asynq.RedisClientOpt{Addr: addr}, asynq.Config{
		Concurrency: 1, Queues: map[string]int{queue: 1}, TaskCheckInterval: 100 * time.Millisecond,
		ShutdownTimeout: 2 * time.Second,
	})
	mux := asynq.NewServeMux()
	mux.HandleFunc("shutdown-proof", func(context.Context, *asynq.Task) error {
		fmt.Println("KH_PROCESSED")
		return nil
	})
	require.NoError(t, startAsynqPool(srv, mux))
	fmt.Println("KH_READY")
	<-signals
	fmt.Println("KH_APP_DRAINING")
	scanner := bufio.NewScanner(os.Stdin)
	require.True(t, scanner.Scan())
	require.Equal(t, "cleanup", scanner.Text())
	srv.Shutdown()
	fmt.Println("KH_CLEANED")
}
