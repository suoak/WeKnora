package feishusynccontrol

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The mutation is an explicit, isolated SDK-equivalent encoding fixture after
// the actual Consumer read and before the health Lua; no production Redis is
// accessed and this does not claim that every observed conflict is encoding-only.
func TestAutomaticServiceNativeSQLSingleHealthReadBoundaryReprobe(t *testing.T) {
	for _, boundary := range []int{1, 2} {
		name := map[int]string{1: "initial_health", 2: "pre_renew_health"}[boundary]
		t.Run(name, func(t *testing.T) {
			_, s, r := renewableSQLService(t)
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			before, _, err := s.current(ctx)
			require.NoError(t, err)
			key := "asynq:servers:{" + before.WorkerID + ":" + before.ConsumerServerID + "}"
			probe := s.c.ApprovedConsumers
			calls, audits := 0, 0
			samples := s.stable.samples
			s.c.ApprovedConsumers = func(probeCtx context.Context) ([]string, error) {
				calls++
				ids, probeErr := probe(probeCtx)
				if calls == boundary {
					require.NoError(t, probeErr)
					reorderActualConsumerMetadata(t, r, key)
				}
				return ids, probeErr
			}
			s.c.Audit = func(event Event) error {
				if event.Kind == "feishu_automatic_read_boundary" {
					audits++
					require.Equal(t, boundary, calls, "audit must precede the second full probe")
					require.Equal(t, "consumer_read_conflict_reprobe", event.Reason)
					var diagnostic map[string]any
					require.NoError(t, json.Unmarshal([]byte(event.ConsumerDiagnostic), &diagnostic))
					require.Equal(t, "health_consumer_read_conflict", diagnostic["probe_class"])
				}
				return nil
			}
			require.NoError(t, s.c.Step(ctx), "one bounded full revalidation must retain the original identity")
			require.Equal(t, 3, calls, "one extra complete probe, never recursive retry")
			require.Equal(t, 1, audits)
			require.Equal(t, samples+1, s.stable.samples, "a re-read is not an extra stable-window sample")
			require.Positive(t, r.PTTL(ctx, s.c.Guard.PermitKey()).Val())
			after, _, err := s.current(ctx)
			require.NoError(t, err)
			require.Equal(t, before.Epoch, after.Epoch)
			rows, err := r.XRange(ctx, s.c.prefix+"system_journal", "-", "+").Result()
			require.NoError(t, err)
			found := 0
			for _, row := range rows {
				if row.Values["event"] == "automatic_consumer_read_reprobe" {
					found++
				}
			}
			require.Equal(t, 1, found, "the first failed read must remain in the untrimmed journal")
		})
	}
}
