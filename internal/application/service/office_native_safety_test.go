//go:build anydoc

package service

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The synthetic source/model is local to this test, but admission uses the
// actual Redis guard, registered controller, TTLs and SQL fencing callback.
// No permit/stop/lease key is written or cleared by this fixture.
func officeNativeSafety(t *testing.T, db *gorm.DB, sourceID, endpoint string) {
	t.Helper()
	address := os.Getenv("FEISHU_SAFETY_REDIS_ADDR")
	require.NotEmpty(t, address, "a real disposable Redis service is required")
	client := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1})
	u, err := url.Parse(endpoint)
	require.NoError(t, err)
	policy := feishusafety.DefaultConfig()
	policy.Namespace = "office-native-" + uuid.NewString()
	policy.EndpointHost = u.Hostname()
	guard, err := feishusafety.New(client, policy)
	require.NoError(t, err)
	previous := feishusafety.Default()
	feishusafety.InstallDefault(guard)
	t.Cleanup(func() { feishusafety.InstallDefault(previous); require.NoError(t, client.Close()) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	sourceSHA := os.Getenv("GITHUB_SHA")
	if sourceSHA == "" {
		sourceSHA = strings.Repeat("a", 40)
	}
	controller, err := feishusynccontrol.New(client, guard, feishusynccontrol.Config{
		WorkerID: "office-native-worker", ExpectedWorkers: []string{"office-native-worker"},
		SourceSHA: sourceSHA, Scope: sourceID, Maintenance: 1,
	}, func(stop context.Context) error { _, e := repository.FenceLegacySyncExecutions(stop, db); return e }, func(feishusynccontrol.Event) error { return nil })
	require.NoError(t, err)
	require.Error(t, guard.Check(feishusafety.WithScope(ctx, sourceID)), "closed until controller validates inventory and health")
	refresh := func() error {
		now, e := client.Time(ctx).Result()
		if e != nil {
			return e
		}
		yes, no, zero := true, false, 0
		data, e := json.Marshal(feishusynccontrol.Metrics{ObservedUnixMS: now.UnixMilli(), GatewayBudgetKnown: &yes, GatewayHealthy: &yes, ConsumerHealthy: &yes, OnlineHealthy: &yes, ScopeViolations: &zero, QuotaExceeded: &no, OnlineP95Ratio: 1})
		if e != nil {
			return e
		}
		if e = client.Set(ctx, controller.MetricsKey(), data, 2*time.Second).Err(); e != nil {
			return e
		}
		return controller.Step(ctx)
	}
	require.NoError(t, refresh())
	require.NoError(t, guard.Check(feishusafety.WithScope(ctx, sourceID)))
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if e := refresh(); e != nil && ctx.Err() == nil {
					t.Logf("controller retained refusal: %v", e)
				}
			}
		}
	}()
	t.Cleanup(func() { cancel(); <-done })
}
