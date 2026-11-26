package feishusafety

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Each old fixture historically owned an independent miniredis, including its
// fixed Asynq queue keys. A shared real daemon breaks that isolation. Start an
// owned real Redis process instead; never FLUSH a shared Redis or erase a trip
// during an assertion to make an emergency/recovery case pass.
func ownedRedisProcess(t *testing.T, executable string) *redis.Options {
	t.Helper()
	// A short directory avoids Unix socket path length limits on long subtests.
	dir, err := os.MkdirTemp("", "knowhub-test-redis-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "redis.sock")
	command := exec.Command(executable, "--port", "0", "--unixsocket", socket,
		"--unixsocketperm", "700", "--save", "", "--appendonly", "no", "--loglevel", "warning")
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		_ = os.RemoveAll(dir) // Generated, exact, test-owned temporary directory.
		t.Fatal(err)
	}
	options := &redis.Options{
		Network: "unix", Addr: socket, MaxRetries: -1,
		DialTimeout: 200 * time.Millisecond, ReadTimeout: 200 * time.Millisecond, WriteTimeout: 200 * time.Millisecond,
	}
	probe := redis.NewClient(options)
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	t.Cleanup(func() {
		// Dispose only this process AFTER assertions, not an existing environment.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = probe.ShutdownNoSave(ctx).Err()
		_ = probe.Close()
		select {
		case <-finished:
		case <-time.After(time.Second):
			_ = command.Process.Kill()
			<-finished
		}
		_ = os.RemoveAll(dir)
	})
	ready := time.Now().Add(2 * time.Second)
	for {
		if err := probe.Ping(context.Background()).Err(); err == nil {
			t.Log("Redis backend: independent real Redis process, private socket, no shared-state reset")
			return options
		}
		if time.Now().After(ready) {
			t.Fatal("owned real Redis did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
