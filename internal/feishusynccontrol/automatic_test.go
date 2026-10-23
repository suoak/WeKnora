package feishusynccontrol

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/hibiken/asynq"
)

func TestAutomaticRegistryAndConsumerLoss(t *testing.T) {
	f := newControlFixture(t)
	cfg := f.config
	cfg.Automatic = true
	cfg.Scope = "all"
	cfg.Maintenance = 4
	cfg.ExpectedWorkers = nil
	newWorker := func(id string) *Controller {
		cfg.WorkerID = id
		namespace := strings.TrimSuffix(strings.TrimPrefix(f.guard.PermitKey(), "feishu:safety:{"), "}:permit")
		g, err := feishusafety.New(
			f.client,
			feishusafety.Config{
				Namespace:          namespace,
				EndpointHost:       "gateway.invalid",
				InFlight:           1,
				RPM:                5,
				RPS:                1,
				TPM:                1000,
				WaitBudgetDuration: time.Second,
				TokenBoundVerified: true,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		c, err := New(f.client, g, cfg, func(context.Context) error { return nil }, func(Event) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		c.Consumers = func(context.Context) ([]string, error) { return []string{"worker-a", "worker-b"}, nil }
		return c
	}
	a, b := newWorker("worker-a"), newWorker("worker-b")
	if a.Step(context.Background()) == nil {
		t.Fatal("missing controller admitted")
	}
	if err := b.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Guard.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	// An actual consumer disappearing does not shrink registry to hide its loss.
	a.Consumers = func(context.Context) ([]string, error) { return []string{"worker-a"}, nil }
	if a.Step(context.Background()) == nil {
		t.Fatal("consumer loss admitted")
	}
	if b.Guard.Check(context.Background()) == nil {
		t.Fatal("trip failed to close other process")
	}
	a.Consumers = b.Consumers
	if a.Step(context.Background()) == nil {
		t.Fatal("trip automatically recovered")
	}
}

func TestAutomaticActualAsynqConsumer(t *testing.T) {
	f := newControlFixture(t)
	probe := SyncConsumers(f.client)
	if _, err := probe(context.Background()); err == nil {
		t.Fatal("missing asynq registry accepted")
	}
	worker := asynq.NewServer(
		asynq.RedisClientOpt{Addr: f.server.Addr()},
		asynq.Config{Concurrency: 4, Queues: map[string]int{"sync": 2, "low": 1}, ShutdownTimeout: time.Second},
	)
	if err := worker.Start(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })); err != nil {
		t.Fatal(err)
	}
	defer worker.Shutdown()
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	cfg := f.config
	cfg.Automatic = true
	cfg.Scope = "all"
	cfg.ExpectedWorkers = nil
	cfg.Maintenance = 4
	cfg.WorkerID = fmt.Sprintf("%s:%d", host, os.Getpid())
	c, err := New(f.client, f.guard, cfg, func(context.Context) error { return nil }, func(Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	c.Consumers = probe
	deadline := time.Now().Add(3 * time.Second)
	for c.Step(context.Background()) != nil {
		if time.Now().After(deadline) {
			t.Fatal("real asynq consumer did not admit")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err = f.guard.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	worker.Shutdown()
	if c.Step(context.Background()) == nil {
		t.Fatal("stopped actual consumer admitted")
	}
	if f.guard.Check(context.Background()) == nil {
		t.Fatal("consumer stop did not freeze outbound")
	}
	f.server.Close()
	if c.Step(context.Background()) == nil {
		t.Fatal("redis loss permitted")
	}
	if f.guard.Check(context.Background()) == nil {
		t.Fatal("redis loss failed open")
	}
}

func TestAutomaticNoConsumerBootstrapAndAuditFailure(t *testing.T) {
	f := newControlFixture(t)
	cfg := f.config
	cfg.Automatic = true
	cfg.ExpectedWorkers = nil
	cfg.Scope = "all"
	c, err := New(f.client, f.guard, cfg, func(context.Context) error { return nil }, func(Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	c.Consumers = func(context.Context) ([]string, error) { return nil, errors.New("absent") }
	if c.Step(context.Background()) == nil {
		t.Fatal("no consumer admitted")
	}
	if f.client.Exists(context.Background(), "asynq:{sync}:paused").Val() != 0 {
		t.Fatal("bootstrap unnecessarily paused fresh queue")
	}
	c.started = time.Now().Add(-time.Minute)
	if c.Step(context.Background()) == nil {
		t.Fatal("expired bootstrap admitted")
	}
	if f.client.Exists(context.Background(), "asynq:{sync}:paused").Val() != 1 {
		t.Fatal("loss did not pause")
	}
}
