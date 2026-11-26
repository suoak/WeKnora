package feishusynccontrol

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type renewBoundaryHook struct {
	armed  atomic.Bool
	mutate func()
}

func (h *renewBoundaryHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *renewBoundaryHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *renewBoundaryHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		args := cmd.Args()
		if cmd.Name() == "evalsha" && len(args) > 1 && args[1] == renewSystemService.Hash() &&
			h.armed.CompareAndSwap(true, false) {
			h.mutate()
		}
		return next(ctx, cmd)
	}
}

func TestAutomaticServiceNativeSQLFinalRenewConflictsStillFailClosed(t *testing.T) {
	for _, mode := range []string{
		"metadata_encoding", "metadata_missing", "root_blocked", "membership", "lease", "operator_pause", "canceled",
	} {
		t.Run(mode, func(t *testing.T) {
			_, s, r := renewableSQLService(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			before, _, err := s.current(ctx)
			require.NoError(t, err)
			key := "asynq:servers:{" + before.WorkerID + ":" + before.ConsumerServerID + "}"
			expected := ""
			hook := &renewBoundaryHook{mutate: func() {
				switch mode {
				case "metadata_encoding":
					reorderActualConsumerMetadata(t, r, key)
					expected = "renew_consumer_read_conflict"
				case "metadata_missing":
					require.NoError(t, r.Del(ctx, key).Err()) // This disposable Redis only.
					expected = "renew_consumer_unavailable"
				case "root_blocked":
					blocked := before
					blocked.Phase = "blocked"
					data, marshalErr := json.Marshal(blocked)
					require.NoError(t, marshalErr)
					require.NoError(t, r.Set(ctx, s.c.prefix+"system_service", data, 0).Err())
					expected = "renew_root_read_conflict"
				case "membership":
					require.NoError(t, r.Incr(ctx, s.c.prefix+"membership_revision").Err())
					expected = "renew_identity_changed"
				case "lease":
					require.NoError(t, r.ZAdd(ctx, s.c.prefix+"leases", redis.Z{
						Score: float64(time.Now().Add(-time.Second).UnixMilli()), Member: before.WorkerID,
					}).Err())
					expected = "renew_lease_expired"
				case "operator_pause":
					require.NoError(t, r.Set(ctx, "asynq:{approved_sync}:paused", "operator_owned_hold", 0).Err())
					expected = "renew_operator_queue_pause"
				case "canceled":
					cancel()
					expected = "renew_context_canceled"
				}
			}}
			r.AddHook(hook)
			hook.armed.Store(true)
			require.Error(t, s.c.Step(ctx))
			require.False(t, hook.armed.Load(), "must inject at the actual final EVALSHA, not an earlier failure")
			require.NotEmpty(t, expected)
			require.Equal(t, expected, s.c.consumerDiagnostic.ProbeClass)
			require.True(t, s.c.Guard.DependencySuspended())
			require.Zero(t, r.Exists(context.Background(), s.c.Guard.PermitKey()).Val())
			require.Error(t, s.c.Guard.Check(context.Background()))
			if mode == "operator_pause" {
				require.Equal(
					t,
					"operator_owned_hold",
					r.Get(context.Background(), "asynq:{approved_sync}:paused").Val(),
				)
			}
			if mode == "root_blocked" {
				current, _, readErr := s.current(context.Background())
				require.NoError(t, readErr)
				require.Equal(t, "blocked", current.Phase, "never overwrite a conflicting state")
			}
			journal, journalErr := r.XRange(context.Background(), s.c.prefix+"system_journal", "-", "+").Result()
			require.NoError(t, journalErr)
			require.Contains(t, journal[len(journal)-1].Values["consumer_diagnostic"], expected)
		})
	}
}

type diagnosticSQLState string

func (s diagnosticSQLState) Error() string    { return "sensitive SQL parameter must not escape" }
func (s diagnosticSQLState) SQLState() string { return string(s) }

func TestAutomaticServiceRenewDiagnosticsNeverExposeUnderlyingErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.Canceled, "canary_context_canceled"},
		{context.DeadlineExceeded, "canary_context_deadline"},
		{gorm.ErrRecordNotFound, "canary_record_not_found"},
		{diagnosticSQLState("42703"), "canary_sql_schema"},
		{diagnosticSQLState("40001"), "canary_sql_concurrency"},
		{diagnosticSQLState("secret_token_value"), "canary_sql_query"},
		{errors.New("ERR SYSTEM_STATE_CHANGED"), "canary_root_read_conflict"},
		{errors.New("ERR SYSTEM_CANARY_CHANGED"), "canary_root_read_conflict"},
		{redis.Nil, "canary_source_registration_absent"},
		{errors.New("unknown token=private-secret SQL(body)"), "canary_internal_error"},
	} {
		require.Equal(t, tc.want, systemServiceCanaryClass(tc.err))
		require.NotContains(t, systemServiceRenewClass(tc.err), "secret")
	}
}

func TestAutomaticServiceWaitDedupRetainsDistinctFailureClasses(t *testing.T) {
	s, r, _ := nativeAutomaticService(t)
	ctx := context.Background()
	s.c.stage(ctx, "system_service_renew")
	s.c.consumerDiagnostic = newConsumerEvidence(ctx, r, true)
	classes := []string{
		"renew_root_read_conflict",
		"renew_consumer_read_conflict",
		"renew_consumer_read_conflict",
	}
	for _, class := range classes {
		s.c.consumerDiagnostic.ProbeClass = class
		require.ErrorIs(t, s.withdraw(ctx, "runtime_or_queue_changed"), ErrNotReady)
	}
	journal, err := r.XRange(ctx, s.c.prefix+"system_journal", "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, journal, 2, "same failure is deduplicated; a distinct safe class is not lost")
	for _, row := range journal {
		diagnostic := row.Values["consumer_diagnostic"].(string)
		require.Contains(t, diagnostic, "renew_")
	}
	require.Error(t, s.c.Guard.Check(ctx))
	require.Zero(t, r.Exists(ctx, s.c.Guard.PermitKey()).Val())
	// Audit failures must not be converted into a successful Step/permit.
	s.c.Audit = func(Event) error { return errors.New("controlled audit failure " + uuid.NewString()) }
	s.c.consumerDiagnostic.ProbeClass = "renew_identity_changed"
	require.ErrorIs(t, s.withdraw(ctx, "runtime_or_queue_changed"), ErrNotReady)
	require.True(t, s.c.Guard.DependencySuspended())
	require.Zero(t, r.Exists(ctx, s.c.Guard.PermitKey()).Val())
}
