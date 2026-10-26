package feishusafety

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
)

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
