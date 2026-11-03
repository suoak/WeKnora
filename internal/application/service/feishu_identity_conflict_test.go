package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/stretchr/testify/require"
)

func TestFeishuIdentityConflictRetainsFenceAndAction(t *testing.T) {
	err := fmt.Errorf("prepare: %w", repository.ErrPersonalAuthModeConflict)
	require.True(t, errors.Is(err, repository.ErrPersonalIdentity))
	require.Equal(t, "source_identity_conflict", feishuV2Error(err))
	action, retryable := feishuFailureAction(feishuV2Error(err))
	require.Equal(t, "contact_admin", action)
	require.False(t, retryable)
	// Other identity changes must not be misreported as a cross-mode collision.
	require.Equal(t, "operation_failed", feishuV2Error(repository.ErrPersonalIdentity))
}
