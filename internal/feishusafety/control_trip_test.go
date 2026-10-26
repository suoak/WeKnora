package feishusafety

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"
)

type unavailableScriptCache struct{ attempts atomic.Int32 }

func (h *unavailableScriptCache) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *unavailableScriptCache) ProcessPipelineHook(
	next redis.ProcessPipelineHook,
) redis.ProcessPipelineHook {
	return next
}

func (h *unavailableScriptCache) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "evalsha" {
			h.attempts.Add(1)
			return errors.New("script_cache_probe_unavailable")
		}
		return next(ctx, cmd)
	}
}

func TestControlTripColdScriptCacheDoesNotBlockStop(t *testing.T) {
	g, control := fixture(t, Config{})
	hook := &unavailableScriptCache{}
	control.AddHook(hook)
	ctx := context.Background()
	if err := g.TripUsing(ctx, control, "audit_unavailable"); err != nil {
		t.Fatal(err)
	}
	if hook.attempts.Load() != 0 || control.Get(ctx, g.key("trip")).Val() != "audit_unavailable" ||
		control.Exists(ctx, g.PermitKey()).Val() != 0 {
		t.Fatal("atomic stop incorrectly depended on a script-cache acknowledgement")
	}
}

func TestControlTripDoesNotDependOnBusinessRedisClient(t *testing.T) {
	g, control := fixture(t, Config{})
	opts := *control.Options()
	opts.MaxRetries = -1
	broken := redis.NewClient(&opts)
	if err := broken.Close(); err != nil {
		t.Fatal(err)
	}
	g.client = broken
	ctx := context.Background()
	if err := g.TripUsing(ctx, control, "audit_unavailable"); err != nil {
		t.Fatal(err)
	}
	if control.Get(ctx, g.key("trip")).Val() != "audit_unavailable" ||
		control.Exists(ctx, g.PermitKey()).Val() != 0 ||
		g.Check(ctx) == nil {
		t.Fatal("business client failure prevented durable stop or left outbound permission")
	}
	if err := g.TripUsing(ctx, control, "later_reason"); err != nil {
		t.Fatal(err)
	}
	if control.Get(ctx, g.key("trip")).Val() != "audit_unavailable" ||
		control.XLen(ctx, g.key("audit")).Val() != 1 {
		t.Fatal("isolated transport replaced the first cause or duplicated audit")
	}
}

func TestControlTripTransportFailureStillClosesLocally(t *testing.T) {
	g, _ := fixture(t, Config{})
	if g.TripUsing(context.Background(), nil, "audit_unavailable") == nil ||
		g.Check(context.Background()) == nil {
		t.Fatal("missing control transport left local outbound open")
	}
}
