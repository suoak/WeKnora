package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type controlFixture struct {
	server *miniredis.Miniredis
	client *redis.Client
	guard  *feishusafety.Guard
	config Config
	stops  int
	events []Event
}

func newControlFixture(t *testing.T) *controlFixture {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	guard, err := feishusafety.New(
		client,
		feishusafety.Config{
			Namespace:          "control-" + uuid.NewString(),
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
	return &controlFixture{
		server: server,
		client: client,
		guard:  guard,
		config: Config{
			WorkerID:        "worker-a",
			ExpectedWorkers: []string{"worker-a", "worker-b"},
			SourceSHA:       strings.Repeat("a", 40),
			Scope:           uuid.NewString(),
			Maintenance:     1,
		},
	}
}

func (f *controlFixture) controller(t *testing.T, id, scope string) *Controller {
	t.Helper()
	cfg := f.config
	cfg.WorkerID = id
	cfg.Scope = scope
	c, err := New(
		f.client,
		f.guard,
		cfg,
		func(context.Context) error { f.stops++; return nil },
		func(event Event) error { f.events = append(f.events, event); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *controlFixture) metrics(t *testing.T, c *Controller, mutate func(*Metrics)) {
	t.Helper()
	yes, no, zero := true, false, 0
	now, err := f.client.Time(context.Background()).Result()
	if err != nil {
		t.Fatal(err)
	}
	m := Metrics{
		ObservedUnixMS:     now.UnixMilli(),
		GatewayBudgetKnown: &yes,
		GatewayHealthy:     &yes,
		ConsumerHealthy:    &yes,
		OnlineHealthy:      &yes,
		ScopeViolations:    &zero,
		QuotaExceeded:      &no,
		OnlineP95Ratio:     1,
	}
	if mutate != nil {
		mutate(&m)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.client.Set(context.Background(), c.MetricsKey(), raw, 2*time.Second).Err(); err != nil {
		t.Fatal(err)
	}
}

func (f *controlFixture) admit(t *testing.T) (*Controller, *Controller) {
	t.Helper()
	a := f.controller(t, "worker-a", f.config.Scope)
	b := f.controller(t, "worker-b", f.config.Scope)
	f.metrics(t, a, nil)
	if err := a.Step(context.Background()); err == nil {
		t.Fatal("incomplete inventory admitted")
	}
	if err := b.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.guard.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a, b
}

func TestControllerTwoWorkersPermitAndExpiry(t *testing.T) {
	f := newControlFixture(t)
	f.admit(t)
	// A permit never resumes a queue paused during closed bootstrap.
	paused, err := f.client.Exists(context.Background(), "asynq:{sync}:paused").Result()
	if err != nil || paused != 1 {
		t.Fatal("controller resumed queue", paused, err)
	}
	f.server.FastForward(time.Second)
	if f.guard.Check(context.Background()) == nil {
		t.Fatal("controller loss did not expire permit")
	}
}

func TestControllerScopeNoneAndMismatch(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		f := newControlFixture(t)
		a := f.controller(t, "worker-a", "none")
		b := f.controller(t, "worker-b", "none")
		f.metrics(t, a, nil)
		_ = a.Step(context.Background())
		_ = b.Step(context.Background())
		if f.guard.Check(context.Background()) == nil {
			t.Fatal("scope none granted outbound permission")
		}
	})
	t.Run("mismatch", func(t *testing.T) {
		f := newControlFixture(t)
		a := f.controller(t, "worker-a", f.config.Scope)
		b := f.controller(t, "worker-b", "none")
		f.metrics(t, a, nil)
		_ = a.Step(context.Background())
		if b.Step(context.Background()) == nil {
			t.Fatal("policy mismatch accepted")
		}
		if f.guard.Check(context.Background()) == nil {
			t.Fatal("mismatch left permit")
		}
	})
}

func TestControllerUnhealthyAndMissingMetrics(t *testing.T) {
	for _, kind := range []string{
		"missing", "stale", "gateway_unknown", "gateway_down", "online_p95",
		"quota_or_429", "scope_violation", "missing_field", "consumer_unhealthy", "consumer_unknown",
	} {
		t.Run(kind, func(t *testing.T) {
			f := newControlFixture(t)
			a, _ := f.admit(t)
			if kind == "missing" {
				_ = f.client.Del(context.Background(), a.MetricsKey()).Err()
			} else {
				f.metrics(t, a, func(m *Metrics) {
					bad, one := false, 1
					switch kind {
					case "stale":
						m.ObservedUnixMS -= 3000
					case "gateway_unknown":
						m.GatewayBudgetKnown = &bad
					case "gateway_down":
						m.GatewayHealthy = &bad
					case "consumer_unhealthy":
						m.ConsumerHealthy = &bad
					case "consumer_unknown":
						m.ConsumerHealthy = nil
					case "online_p95":
						m.OnlineP95Ratio = 1.21
					case "quota_or_429":
						yes := true
						m.QuotaExceeded = &yes
					case "scope_violation":
						m.ScopeViolations = &one
					case "missing_field":
						m.QuotaExceeded = nil
					}
				})
			}
			if a.Step(context.Background()) == nil {
				t.Fatal("unhealthy metrics accepted")
			}
			if f.guard.Check(context.Background()) == nil {
				t.Fatal("unhealthy state left permit")
			}
			if f.stops < 2 || len(f.events) < 2 {
				t.Fatal("stop/audit missing", f.stops, f.events)
			}
			f.metrics(t, a, nil)
			if a.Step(context.Background()) == nil {
				t.Fatal("latched stop restarted automatically")
			}
		})
	}
}

func TestControllerDuplicateIdentityAndMissingWorker(t *testing.T) {
	t.Run("duplicate", func(t *testing.T) {
		f := newControlFixture(t)
		a, _ := f.admit(t)
		duplicate := f.controller(t, a.Config.WorkerID, a.Config.Scope)
		if duplicate.Step(context.Background()) == nil {
			t.Fatal("duplicate identity accepted")
		}
		if f.guard.Check(context.Background()) == nil {
			t.Fatal("duplicate identity failed open")
		}
	})
	t.Run("lost_worker", func(t *testing.T) {
		f := newControlFixture(t)
		a, _ := f.admit(t)
		_ = f.client.ZAdd(context.Background(), a.prefix+"leases", redis.Z{Score: 0, Member: "worker-b"}).Err()
		if a.Step(context.Background()) == nil {
			t.Fatal("missing live worker accepted")
		}
		if f.guard.Check(context.Background()) == nil {
			t.Fatal("missing worker failed open")
		}
	})
}

func TestControllerAuditFailureRetried(t *testing.T) {
	f := newControlFixture(t)
	a, _ := f.admit(t)
	attempts := 0
	a.Audit = func(Event) error { attempts++; return errors.New("isolated alert unavailable") }
	_ = a.EmergencyStop(context.Background())
	_ = a.EmergencyStop(context.Background())
	if attempts != 2 {
		t.Fatalf("failed alert attempts=%d, want retry", attempts)
	}
}

func TestControllerAlertHeartbeatFailureClosesPermit(t *testing.T) {
	f := newControlFixture(t)
	a, _ := f.admit(t)
	a.lastAlertAck = time.Time{}
	a.Audit = func(Event) error { return errors.New("isolated alert unavailable") }
	if a.Step(context.Background()) == nil || f.guard.Check(context.Background()) == nil {
		t.Fatal("lost acknowledged alert path left outbound permission")
	}
}

func TestControllerMetricsRejectAmbiguousJSON(t *testing.T) {
	for _, raw := range []string{
		`{"observed_unix_ms":1,"observed_unix_ms":2}`,
		`{} {}`,
		`{"unknown":true}`,
		`[]`,
	} {
		var metrics Metrics
		if decodeMetrics(raw, &metrics) == nil {
			t.Fatalf("ambiguous metrics accepted: %s", raw)
		}
	}
}

func TestControllerTrackCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	untrack := Track(cancel)
	defer untrack()
	CancelLocal()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("active local execution not canceled")
	}
}
