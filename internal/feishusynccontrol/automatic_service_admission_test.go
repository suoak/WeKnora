package feishusynccontrol

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAutomaticServiceCanaryAdmissionDefersOnlyExactRedisReply(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	for _, code := range []string{
		"SYSTEM_CANARY_OTHER_SOURCE", "SYSTEM_SOURCE_BINDING_CHANGED", "SYSTEM_SOURCE_BACKOFF",
		"SYSTEM_SOURCE_NOT_READY", "SYSTEM_CANARY_OTHER_SOURCE_EXTRA",
	} {
		t.Run(code, func(t *testing.T) {
			// Exercise the actual Redis error type; a remote document error
			// containing the same text must never become a deferred admission.
			err := client.Eval(context.Background(), "return redis.error_reply(ARGV[1])", nil, code).Err()
			require.Error(t, err)
			classified := automaticSourceAdmissionError(err)
			require.ErrorIs(t, classified, ErrNotReady)
			require.Equal(t, code == "SYSTEM_CANARY_OTHER_SOURCE", errors.Is(classified, ErrAutomaticCanaryPending))
		})
	}
	for _, err := range []error{
		errors.New("SYSTEM_CANARY_OTHER_SOURCE"), context.Canceled, context.DeadlineExceeded,
	} {
		require.NotErrorIs(t, automaticSourceAdmissionError(err), ErrAutomaticCanaryPending)
	}
	require.NoError(t, automaticSourceAdmissionError(nil))
}
