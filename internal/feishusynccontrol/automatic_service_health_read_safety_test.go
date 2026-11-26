package feishusynccontrol

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type healthBoundaryJournalFailure struct{ stream string }

func (h healthBoundaryJournalFailure) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return next(ctx, network, address)
	}
}

func (h healthBoundaryJournalFailure) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h healthBoundaryJournalFailure) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "xadd" && len(cmd.Args()) > 1 && cmd.Args()[1] == h.stream {
			return errors.New("fixture journal unavailable")
		}
		return next(ctx, cmd)
	}
}

func TestAutomaticServiceNativeSQLHealthReadReprobeStillFailsClosed(t *testing.T) {
	for _, mode := range []string{
		"second_conflict", "second_missing_metadata", "second_corrupt_metadata",
		"second_membership", "second_incarnation", "audit_failure", "audit_deadline", "journal_failure", "both_boundaries",
	} {
		t.Run(mode, func(t *testing.T) {
			_, s, r := renewableSQLService(t)
			readCtx := context.Background()
			before, _, err := s.current(readCtx)
			require.NoError(t, err)
			key := "asynq:servers:{" + before.WorkerID + ":" + before.ConsumerServerID + "}"
			ctx, cancel := context.WithTimeout(readCtx, 500*time.Millisecond)
			defer cancel()
			probe := s.c.ApprovedConsumers
			calls, audits := 0, 0
			s.c.ApprovedConsumers = func(probeCtx context.Context) ([]string, error) {
				calls++
				ids, probeErr := probe(probeCtx)
				if calls == 1 || mode == "second_conflict" && calls == 2 || mode == "both_boundaries" && calls == 3 {
					require.NoError(t, probeErr)
					reorderActualConsumerMetadata(t, r, key)
				}
				return ids, probeErr
			}
			s.c.Audit = func(event Event) error {
				if event.Kind != "feishu_automatic_read_boundary" {
					return nil
				}
				audits++
				require.Equal(t, 1, calls)
				switch mode {
				case "second_missing_metadata":
					require.NoError(t, r.Del(readCtx, key).Err())
				case "second_corrupt_metadata":
					require.NoError(t, r.Set(readCtx, key, "fixture-private-invalid-protobuf", 10*time.Second).Err())
				case "second_membership":
					require.NoError(t, r.Incr(readCtx, s.c.prefix+"membership_revision").Err())
				case "second_incarnation":
					require.NoError(t, r.HSet(readCtx, s.c.prefix+"workers", s.c.Config.WorkerID,
						s.c.fingerprint+":"+uuid.NewString()).Err())
				case "audit_failure":
					return errors.New("fixture private audit error")
				case "audit_deadline":
					<-event.Context().Done()
				}
				return nil
			}
			if mode == "journal_failure" {
				r.AddHook(healthBoundaryJournalFailure{stream: s.c.prefix + "system_journal"})
			}
			require.Error(t, s.c.Step(ctx))
			expectedCalls := 2
			if mode == "both_boundaries" {
				expectedCalls = 3
			}
			if mode == "audit_failure" || mode == "audit_deadline" || mode == "journal_failure" {
				expectedCalls = 1
			}
			require.Equal(
				t,
				expectedCalls,
				calls,
				"one extra health probe for the entire Step, never a second revalidation",
			)
			if mode == "journal_failure" {
				require.Zero(t, audits)
			} else {
				require.Equal(t, 1, audits)
			}
			require.Zero(t, s.stable.samples)
			require.Zero(t, r.Exists(readCtx, s.c.Guard.PermitKey()).Val())
			require.Error(t, s.c.Guard.Check(readCtx))
		})
	}
}

func TestAutomaticServiceNativeSQLNonConflictHealthFailuresAreNotReprobed(t *testing.T) {
	for _, mode := range []string{"missing", "registry_expired", "identity", "redis_read", "deadline", "canceled", "similar_plain_error"} {
		t.Run(mode, func(t *testing.T) {
			_, s, r := renewableSQLService(t)
			ctx := context.Background()
			before, _, err := s.current(ctx)
			require.NoError(t, err)
			key := "asynq:servers:{" + before.WorkerID + ":" + before.ConsumerServerID + "}"
			probe := s.c.ApprovedConsumers
			calls, audits := 0, 0
			s.c.ApprovedConsumers = func(probeCtx context.Context) ([]string, error) {
				calls++
				switch mode {
				case "redis_read":
					return nil, errors.New("fixture redis read failure")
				case "deadline":
					return nil, context.DeadlineExceeded
				case "canceled":
					return nil, context.Canceled
				case "similar_plain_error":
					return nil, errors.New("SYSTEM_CONSUMER_READ_CONFLICT")
				}
				ids, probeErr := probe(probeCtx)
				require.NoError(t, probeErr)
				switch mode {
				case "missing":
					require.NoError(t, r.Del(ctx, key).Err())
				case "registry_expired":
					require.NoError(
						t,
						r.ZAdd(ctx, "asynq:servers", redis.Z{Score: float64(time.Now().Add(-time.Second).Unix()), Member: key}).
							Err(),
					)
				case "identity":
					require.NoError(t, r.Incr(ctx, s.c.prefix+"membership_revision").Err())
				}
				return ids, probeErr
			}
			s.c.Audit = func(event Event) error {
				if event.Kind == "feishu_automatic_read_boundary" {
					audits++
				}
				return nil
			}
			require.Error(t, s.c.Step(ctx))
			require.Equal(t, 1, calls)
			require.Zero(t, audits)
			require.Zero(t, r.Exists(ctx, s.c.Guard.PermitKey()).Val())
		})
	}
}
