package core

import (
	"errors"
	"net/url"
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/stretchr/testify/require"
)

func TestTransportSafetyLabelsPreserveCauseAndRetryCategory(t *testing.T) {
	for _, tc := range []struct {
		cause error
		label string
	}{
		{feishusafety.ErrControlTemporarilyUnavailable, "safety_control_unavailable"},
		{feishusafety.ErrEmbeddingTemporarilyUnavailable, "safety_embedding_unavailable"},
		{feishusafety.ErrSourceValidationUnavailable, "safety_source_validation_unavailable"},
		{feishusafety.ErrSourceAdmissionChanged, "safety_source_admission_changed"},
		{feishusafety.ErrBudgetBusy, "safety_budget_busy"},
		{feishusafety.ErrClosed, "safety_closed"},
	} {
		wrapped := &url.Error{Op: "Get", URL: "https://secret.invalid/private?token=hidden", Err: tc.cause}
		api := &APIError{Category: ErrorCategoryTransient, Operation: "get_wiki_node", Cause: wrapped}
		require.Equal(t, tc.label, TransportErrorCategory(wrapped))
		require.Same(t, wrapped, api.Cause)
		require.Same(t, wrapped, api.Unwrap())
		require.True(t, errors.Is(api, tc.cause))
		require.ErrorIs(t, api, ErrTransient)
		require.Equal(t, ErrorCategoryTransient, api.Category)
		require.NotContains(t, api.Error(), "hidden")
		require.NotContains(t, api.Error(), "secret.invalid")
	}
}
