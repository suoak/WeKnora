package feishusynccontrol

import (
	"context"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
)

func (f *controlFixture) automaticController(t *testing.T, id string) *Controller {
	t.Helper()
	cfg := f.config
	cfg.Automatic, cfg.ExpectedWorkers, cfg.WorkerID = true, nil, id
	c, err := New(f.client, f.guard, cfg,
		func(context.Context) error { f.stops++; return nil },
		func(e Event) error { f.events = append(f.events, e); return nil })
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Replay 11:31:05: the admitted app sees worker2's controller before worker2
// advertises its Asynq consumer. A healthy bounded join must withdraw permission,
// without interpreting that never-admitted consumer as a previously lost one.
func TestAutomaticLateConsumerJoinWithdrawsPermitWithoutTrip(t *testing.T) {
	f := newControlFixture(t)
	ctx := context.Background()
	a, b := f.automaticController(t, "app:1"), f.automaticController(t, "worker2:1")
	a.Consumers = func(context.Context) ([]string, error) { return []string{"app:1"}, nil }
	b.Consumers = a.Consumers
	if err := a.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.client.Set(ctx, "asynq:{sync}:paused", "prior-stop", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if b.Step(ctx) == nil || a.Step(ctx) == nil {
		t.Fatal("incomplete consumer topology retained permission")
	}
	if f.client.Exists(ctx, a.prefix+"trip").Val() != 0 || f.stops != 0 {
		t.Fatal("healthy joining consumer was treated as a consumer loss")
	}
	if a.Guard.Check(ctx) == nil {
		t.Fatal("outbound opened while worker2 was still starting")
	}
	a.Consumers = func(context.Context) ([]string, error) { return []string{"app:1", "worker2:1"}, nil }
	b.Consumers = a.Consumers
	if err := b.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if f.client.Get(ctx, "asynq:{sync}:paused").Val() != "prior-stop" {
		t.Fatal("consumer join resumed or changed a paused queue")
	}
	a.Consumers = func(context.Context) ([]string, error) { return []string{"app:1"}, nil }
	if a.Step(ctx) == nil || f.client.Exists(ctx, a.prefix+"trip").Val() != 1 {
		t.Fatal("a previously admitted consumer disappeared without tripping")
	}
}

func TestAutomaticConsumerJoinHasSharedDeadline(t *testing.T) {
	f := newControlFixture(t)
	ctx := context.Background()
	a, b := f.automaticController(t, "app:1"), f.automaticController(t, "worker2:1")
	a.Consumers = func(context.Context) ([]string, error) { return []string{"app:1"}, nil }
	b.Consumers = a.Consumers
	if err := a.Step(ctx); err != nil {
		t.Fatal(err)
	}
	_ = b.Step(ctx)
	if err := f.client.ZAdd(ctx, a.prefix+"joining", redis.Z{Score: 0, Member: "worker2:1"}).Err(); err != nil {
		t.Fatal(err)
	}
	if a.Step(ctx) == nil ||
		f.client.Get(ctx, a.prefix+"trip").Val() != "automatic_permit_consumer_start_timeout" {
		t.Fatal("a permanently absent joining consumer did not reach a terminal safety stop")
	}
}

func TestAutomaticSnapshotBeforePeerAdmissionDoesNotInventLoss(t *testing.T) {
	f := newControlFixture(t)
	ctx := context.Background()
	a, b := f.automaticController(t, "app:1"), f.automaticController(t, "worker2:1")
	a.Consumers = func(context.Context) ([]string, error) { return []string{"app:1"}, nil }
	b.Consumers = a.Consumers
	if err := a.Step(ctx); err != nil {
		t.Fatal(err)
	}
	_ = b.Step(ctx) // worker2 is registered, not yet observed by Asynq.
	all := func(context.Context) ([]string, error) { return []string{"app:1", "worker2:1"}, nil }
	b.Consumers = all
	a.Consumers = func(context.Context) ([]string, error) {
		if err := b.Step(ctx); err != nil {
			t.Fatal(err)
		}
		return []string{"app:1"}, nil // snapshot predates the peer's first admission.
	}
	if a.Step(ctx) == nil || f.client.Exists(ctx, a.prefix+"trip").Val() != 0 {
		t.Fatal("an obsolete consumer snapshot was interpreted as a real loss")
	}
	if a.Guard.Check(ctx) == nil {
		t.Fatal("obsolete snapshot did not close outbound")
	}
	a.Consumers = all
	if err := a.Step(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticExpiredAdmittedIncarnationCannotBeSilentlyReplaced(t *testing.T) {
	f := newControlFixture(t)
	ctx := context.Background()
	a := f.automaticController(t, "app:1")
	a.Consumers = func(context.Context) ([]string, error) { return []string{"app:1"}, nil }
	if err := a.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.client.ZAdd(ctx, a.prefix+"leases", redis.Z{Score: 0, Member: "app:1"}).Err(); err != nil {
		t.Fatal(err)
	}
	replacement := f.automaticController(t, "app:1")
	replacement.Consumers = a.Consumers
	if replacement.Step(ctx) == nil ||
		!strings.Contains(f.client.Get(ctx, a.prefix+"trip").Val(), "incarnation_conflict") {
		t.Fatal("a same-ID restart silently healed a previously admitted crash")
	}
}
