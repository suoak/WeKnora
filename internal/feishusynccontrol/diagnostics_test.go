package feishusynccontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAutomaticCanonicalFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		stage string
		err   error
		want  string
	}{
		{"permit", context.DeadlineExceeded, "automatic_permit_context_deadline"},
		{"permit", context.Canceled, "automatic_permit_context_canceled"},
		{"consumer_probe", &probeFailure{stage: "redis_ping", cause: context.DeadlineExceeded}, "automatic_redis_ping_context_deadline"},
		{"consumer_probe", &probeFailure{stage: "asynq_consumer", cause: errConsumerAbsent}, "automatic_asynq_consumer_consumer_absent"},
		{"consumer_probe", &probeFailure{stage: "asynq_consumer", cause: errConsumerInactive}, "automatic_asynq_consumer_consumer_inactive"},
		{"consumer_probe", &probeFailure{stage: "asynq_inspection", cause: errors.New("redis://secret:user@host body")}, "automatic_asynq_inspection_redis_or_probe_error"},
	} {
		if got := automaticFailureReason(tc.stage, tc.err); got != tc.want {
			t.Errorf("got %s want %s", got, tc.want)
		}
	}
	for _, code := range []string{"POLICY_MISMATCH", "WORKER_LOST", "CONSUMER_LOST", "UNREGISTERED_CONSUMER", "CONSUMERS_NOT_READY", "WORKER_ID_CONFLICT", "TRIPPED"} {
		err := fmt.Errorf("ERR %s secret-body-credential", code)
		want := "automatic_permit_" + strings.ToLower(code)
		if got := automaticFailureReason("permit", err); got != want {
			t.Errorf("got %s want %s", got, want)
		}
	}
}

func TestAutomaticProbePastDeadlineIsIdentified(t *testing.T) {
	f := newControlFixture(t)
	cfg := f.config
	cfg.Automatic, cfg.Scope, cfg.ExpectedWorkers = true, "all", nil
	c, err := New(f.client, f.guard, cfg, func(context.Context) error { return nil }, func(Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	c.Consumers = func(context.Context) ([]string, error) { return []string{cfg.WorkerID}, nil }
	if err = c.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Asynq Inspector currently uses Background internally. Reproduce a healthy
	// result returned only after the caller's bounded monitoring step expires.
	c.Consumers = func(ctx context.Context) ([]string, error) { <-ctx.Done(); return []string{cfg.WorkerID}, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if c.Step(ctx) == nil {
		t.Fatal("expired monitoring step unexpectedly admitted")
	}
	if got := f.client.Get(context.Background(), c.prefix+"trip").Val(); got != "automatic_permit_context_deadline" {
		t.Fatal("monitor deadline misdiagnosed", got)
	}
}

func TestAutomaticTripStoresExactCanonicalCause(t *testing.T) {
	f := newControlFixture(t)
	cfg := f.config
	cfg.Automatic = true
	cfg.Scope = "all"
	cfg.ExpectedWorkers = nil
	c, err := New(f.client, f.guard, cfg, func(context.Context) error { return nil }, func(Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	c.Consumers = func(context.Context) ([]string, error) { return []string{cfg.WorkerID}, nil }
	if err = c.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Consumers = func(context.Context) ([]string, error) { return []string{"other-process"}, nil }
	if c.Step(context.Background()) == nil {
		t.Fatal("lost consumer did not stop")
	}
	if got := f.client.Get(context.Background(), c.prefix+"trip").Val(); got != "automatic_permit_consumer_lost" {
		t.Fatal("exact failure lost", got)
	}
}
