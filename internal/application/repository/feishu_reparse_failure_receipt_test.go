package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type reparseSyntheticSQLFailure struct{ state string }

func (e reparseSyntheticSQLFailure) Error() string    { return "SYNTHETIC_SECRET must never persist" }
func (e reparseSyntheticSQLFailure) SQLState() string { return e.state }

func TestSourceDocumentReparseFailureFacts(t *testing.T) {
	ctx := context.Background()
	require.Same(t, ctx, WithSourceDocumentReparseFailure(ctx, nil, false))
	for _, state := range []string{"23505", "password=synthetic-secret", "ab123"} {
		captured := WithSourceDocumentReparseFailure(ctx,
			fmt.Errorf("synthetic wrapper: %w", reparseSyntheticSQLFailure{state: state}), false)
		facts := captured.Value(sourceReparseFailureKey{}).(sourceReparseFailureFacts)
		payload, err := json.Marshal(facts)
		require.NoError(t, err)
		require.NotContains(t, string(payload), "SYNTHETIC_SECRET")
		require.NotContains(t, string(payload), "password")
		require.Len(t, facts.Fingerprint, 64)
		if state == "23505" {
			require.Equal(t, state, facts.SQLState)
		} else {
			require.Empty(t, facts.SQLState)
		}
	}
	large := WithSourceDocumentReparseFailure(ctx, errors.New(strings.Repeat("x", 1<<20)), true)
	facts := large.Value(sourceReparseFailureKey{}).(sourceReparseFailureFacts)
	require.True(t, facts.FingerprintTruncated)
	require.True(t, facts.BudgetBusy)
	timed := WithSourceDocumentReparseFailure(ctx, fmt.Errorf("synthetic: %w", context.DeadlineExceeded), false)
	require.True(t, timed.Value(sourceReparseFailureKey{}).(sourceReparseFailureFacts).Timeout)
	canceled := WithSourceDocumentReparseFailure(ctx, context.Canceled, false)
	require.True(t, canceled.Value(sourceReparseFailureKey{}).(sourceReparseFailureFacts).Canceled)
}
